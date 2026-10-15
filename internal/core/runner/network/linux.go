//go:build linux

package network

import (
	"context"
	"fmt"
	"golang.org/x/sys/unix"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const GatewayPath = "/usr/local/libexec/network-gateway"
const ProxyAddress = "127.0.0.1:3128"

type NamespaceHandle struct {
	PID         int
	NetNSFD     int
	ExecutionID string
	gateway     *Gateway
}

func (g *Gateway) NamespaceHandle(pid, fd int) NamespaceHandle {
	return NamespaceHandle{PID: pid, NetNSFD: fd, ExecutionID: g.executionID, gateway: g}
}
func ApplyScope(ctx context.Context, handle NamespaceHandle, scope NetworkScope) (NetworkReceipt, error) {
	if err := ctx.Err(); err != nil {
		return NetworkReceipt{}, err
	}
	if err := ValidateScope(scope); err != nil {
		return NetworkReceipt{}, err
	}
	if handle.PID <= 0 || handle.NetNSFD < 0 || handle.ExecutionID == "" {
		return NetworkReceipt{}, fmt.Errorf("invalid namespace identity")
	}
	kind, err := unix.IoctlRetInt(handle.NetNSFD, unix.NS_GET_NSTYPE)
	if err != nil || kind != unix.CLONE_NEWNET {
		return NetworkReceipt{}, fmt.Errorf("network namespace descriptor required")
	}
	var held, self, child unix.Stat_t
	if err = unix.Fstat(handle.NetNSFD, &held); err != nil {
		return NetworkReceipt{}, err
	}
	if err = unix.Stat("/proc/self/ns/net", &self); err != nil {
		return NetworkReceipt{}, err
	}
	if err = unix.Stat(fmt.Sprintf("/proc/%d/ns/net", handle.PID), &child); err != nil {
		return NetworkReceipt{}, err
	}
	if held.Ino == self.Ino || held.Ino != child.Ino || held.Dev != child.Dev {
		return NetworkReceipt{}, fmt.Errorf("private owned network namespace required")
	}
	// A fresh request namespace may contain only loopback. Even an otherwise
	// valid namespace FD must not turn an existing routed namespace into none.
	devices, err := os.ReadFile(fmt.Sprintf("/proc/%d/net/dev", handle.PID))
	if err != nil {
		return NetworkReceipt{}, err
	}
	count := 0
	for _, line := range strings.Split(string(devices), "\n") {
		if at := strings.IndexByte(line, ':'); at >= 0 {
			if strings.TrimSpace(line[:at]) != "lo" {
				return NetworkReceipt{}, fmt.Errorf("alternate network interface refused")
			}
			count++
		}
	}
	if count != 1 {
		return NetworkReceipt{}, fmt.Errorf("loopback-only namespace required")
	}
	if scope.Mode == "scoped" {
		g := handle.gateway
		if g == nil || g.executionID != handle.ExecutionID || g.scope.ScopeDigest != scope.ScopeDigest {
			return NetworkReceipt{}, ErrCapability
		}
		g.mu.Lock()
		ready := g.channelReady && !g.closed
		g.mu.Unlock()
		if !ready {
			return NetworkReceipt{}, ErrCapability
		}
		return g.Receipt(), nil
	}
	return NetworkReceipt{ExecutionID: handle.ExecutionID, PolicyDigest: ScopeDigest(scope)}, nil
}
func NewChannel() (*os.File, *os.File, error) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	return os.NewFile(uintptr(fds[0]), "gateway-supervisor"), os.NewFile(uintptr(fds[1]), "gateway-frontend"), nil
}
func unixChannel(file *os.File) (*net.UnixConn, error) {
	c, err := net.FileConn(file)
	if err != nil {
		return nil, err
	}
	channel, ok := c.(*net.UnixConn)
	if !ok {
		c.Close()
		return nil, fmt.Errorf("private Unix channel required")
	}
	return channel, nil
}
func closeFDs(fds []int) {
	for _, fd := range fds {
		unix.Close(fd)
	}
}

// readFrame closes every received descriptor on malformed or truncated input.
// Ancillary space is finite even for a compromised trusted frontend.
func readFrame(channel *net.UnixConn) (byte, int, error) {
	data := make([]byte, 2)
	control := make([]byte, unix.CmsgSpace(4*16))
	n, oobn, flags, _, err := channel.ReadMsgUnix(data, control)
	if err != nil {
		return 0, -1, err
	}
	fds := []int{}
	malformed := false
	messages, parseErr := unix.ParseSocketControlMessage(control[:oobn])
	if parseErr != nil {
		malformed = true
	}
	for _, message := range messages {
		rights, err := unix.ParseUnixRights(&message)
		if err != nil {
			malformed = true
			continue
		}
		fds = append(fds, rights...)
	}
	if flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || n != 1 || malformed || (data[0] == 1 && len(fds) != 0) || (data[0] == 2 && len(fds) != 1) || (data[0] != 1 && data[0] != 2) {
		closeFDs(fds)
		return 0, -1, fmt.Errorf("invalid gateway frame")
	}
	if data[0] == 1 {
		return 1, -1, nil
	}
	unix.CloseOnExec(fds[0])
	kind, err := unix.GetsockoptInt(fds[0], unix.SOL_SOCKET, unix.SO_TYPE)
	domain, domainErr := unix.GetsockoptInt(fds[0], unix.SOL_SOCKET, unix.SO_DOMAIN)
	peer, peerErr := unix.Getpeername(fds[0])
	if err != nil || kind != unix.SOCK_STREAM || domainErr != nil || domain != unix.AF_INET || peerErr != nil {
		closeFDs(fds)
		return 0, -1, fmt.Errorf("connected IPv4 loopback stream required")
	}
	address, ok := peer.(*unix.SockaddrInet4)
	if !ok || address.Addr != [4]byte{127, 0, 0, 1} {
		closeFDs(fds)
		return 0, -1, fmt.Errorf("non-loopback client descriptor")
	}
	return 2, fds[0], nil
}
func (g *Gateway) ServeChannel(file *os.File, ready chan<- error) (result error) {
	notified := false
	defer func() {
		if !notified {
			ready <- result
		}
		if g.ctx.Err() != nil {
			g.Close()
		} else {
			g.revoke("channel_lost")
		}
	}()
	channel, err := unixChannel(file)
	if err != nil {
		return err
	}
	defer channel.Close()
	stop := context.AfterFunc(g.ctx, func() { channel.Close() })
	defer stop()
	frame, fd, err := readFrame(channel)
	if fd >= 0 {
		unix.Close(fd)
	}
	if err != nil || frame != 1 {
		return fmt.Errorf("gateway readiness refused: %v", err)
	}
	g.mu.Lock()
	g.channelReady = true
	g.mu.Unlock()
	ready <- nil
	notified = true
	for {
		frame, fd, err = readFrame(channel)
		if err != nil {
			return err
		}
		if frame != 2 {
			if fd >= 0 {
				unix.Close(fd)
			}
			return fmt.Errorf("repeated readiness frame")
		}
		file := os.NewFile(uintptr(fd), "request-proxy-client")
		conn, err := net.FileConn(file)
		file.Close()
		if err != nil {
			return err
		}
		if err = g.track(conn); err != nil {
			continue
		}
		go g.serveConn(conn)
	}
}

type singleListener struct {
	conn     net.Conn
	accepted bool
	done     chan struct{}
	once     sync.Once
	raw      net.Conn
}

func (l *singleListener) Accept() (net.Conn, error) {
	if !l.accepted {
		l.accepted = true
		return l.conn, nil
	}
	<-l.done
	return nil, net.ErrClosed
}
func (l *singleListener) Close() error {
	l.once.Do(func() { close(l.done); l.raw.Close() })
	return nil
}
func (l *singleListener) Addr() net.Addr { return l.conn.LocalAddr() }

type listenerConn struct {
	net.Conn
	listener *singleListener
}

func (c *listenerConn) Close() error { return c.listener.Close() }
func (g *Gateway) serveConn(conn net.Conn) {
	defer g.untrack(conn)
	listener := &singleListener{conn: conn, raw: conn, done: make(chan struct{})}
	listener.conn = &listenerConn{Conn: conn, listener: listener}
	// Closing the wrapped connection must not recurse through listener.conn.
	server := &http.Server{Handler: g, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 16 << 10}
	server.Serve(listener)
	listener.Close()
}

// FrontendMain runs as the unprivileged trusted helper inside the request
// user/net/mount namespaces. It holds no rules, outbound sockets or credentials.
func FrontendMain() error {
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		return err
	}
	file := os.NewFile(3, "private-supervisor-channel")
	if file == nil {
		return fmt.Errorf("missing private channel")
	}
	channel, err := unixChannel(file)
	file.Close()
	if err != nil {
		return err
	}
	defer channel.Close()
	listener, err := net.Listen("tcp4", ProxyAddress)
	if err != nil {
		return err
	}
	defer listener.Close()
	if _, _, err = channel.WriteMsgUnix([]byte{1}, nil, nil); err != nil {
		return err
	}
	for {
		connection, err := listener.Accept()
		if err != nil {
			return err
		}
		tcp, ok := connection.(*net.TCPConn)
		if !ok {
			connection.Close()
			return fmt.Errorf("unexpected client socket")
		}
		fd, err := tcp.File()
		connection.Close()
		if err != nil {
			return err
		}
		_, _, err = channel.WriteMsgUnix([]byte{2}, unix.UnixRights(int(fd.Fd())), nil)
		fd.Close()
		if err != nil {
			return err
		}
	}
}
