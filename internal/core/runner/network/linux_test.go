//go:build linux

package network

import (
	"bufio"
	"context"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"net"
	"net/http"
	"os"
	"testing"
	"time"
)

func channelPair(t *testing.T) (*os.File, *net.UnixConn) {
	t.Helper()
	parent, child, err := NewChannel()
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.FileConn(child)
	child.Close()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { parent.Close(); conn.Close() })
	return parent, conn.(*net.UnixConn)
}
func TestChannelRequiresReadinessAndRejectsDescriptorSmuggling(t *testing.T) {
	for _, n := range []int{0, 2} {
		t.Run(string(rune('0'+n)), func(t *testing.T) {
			g, err := NewGateway(context.Background(), "channel-test", validNetworkTestScope())
			if err != nil {
				t.Fatal(err)
			}
			defer g.Close()
			file, child := channelPair(t)
			ready := make(chan error, 1)
			done := make(chan error, 1)
			go func() { done <- g.ServeChannel(file, ready) }()
			if _, _, err = child.WriteMsgUnix([]byte{1}, nil, nil); err != nil {
				t.Fatal(err)
			}
			select {
			case err = <-ready:
				if err != nil {
					t.Fatalf("trusted ready refused: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("readiness blocked")
			}
			rights := []byte(nil)
			if n == 2 {
				fd, err := os.Open("/dev/null")
				if err != nil {
					t.Fatal(err)
				}
				defer fd.Close()
				rights = unix.UnixRights(int(fd.Fd()), int(fd.Fd()))
			}
			if _, _, err = child.WriteMsgUnix([]byte{2}, rights, nil); err != nil {
				t.Fatal(err)
			}
			select {
			case err = <-done:
				if err == nil {
					t.Fatal("invalid descriptor frame accepted")
				}
			case <-time.After(time.Second):
				t.Fatal("invalid frame not refused")
			}
		})
	}
}

func TestChannelRechecksPipelinedRequests(t *testing.T) {
	s := validNetworkTestScope()
	s.Destinations[0].Protocol = "http"
	s.Destinations[0].Port = 80
	s.MaxEgressBytes = 4096
	s.ScopeDigest = testDigest(s)
	g, err := NewGateway(context.Background(), "channel-http", s)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	g.dial = func(context.Context, string, string) (net.Conn, error) {
		a, b := net.Pipe()
		go func() {
			defer b.Close()
			if _, err := http.ReadRequest(bufio.NewReader(b)); err == nil {
				io.WriteString(b, "HTTP/1.1 200 OK\r\nContent-Length: 6\r\n\r\ncanary")
			}
		}()
		return a, nil
	}
	parent, child := channelPair(t)
	ready := make(chan error, 1)
	done := make(chan error, 1)
	go func() { done <- g.ServeChannel(parent, ready) }()
	if _, _, err = child.WriteMsgUnix([]byte{1}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err = <-ready; err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.SetDeadline(time.Now().Add(2 * time.Second))
	accepted, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	fd, err := accepted.(*net.TCPConn).File()
	accepted.Close()
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = child.WriteMsgUnix([]byte{2}, unix.UnixRights(int(fd.Fd())), nil)
	fd.Close()
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(client)
	for _, test := range []struct {
		host   string
		status int
	}{{"canary.example", 200}, {"other.example", 403}} {
		if _, err = io.WriteString(client, "GET http://"+test.host+"/ HTTP/1.1\r\nHost: "+test.host+"\r\n\r\n"); err != nil {
			t.Fatal(err)
		}
		response, err := http.ReadResponse(reader, nil)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != test.status {
			t.Fatalf("request reused authority: %d", response.StatusCode)
		}
	}
	g.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("gateway channel survived revocation")
	}
}

func TestApplyScopeRejectsSupervisorNamespace(t *testing.T) {
	file, err := os.Open("/proc/self/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err = ApplyScope(context.Background(), NamespaceHandle{PID: os.Getpid(), NetNSFD: int(file.Fd()), ExecutionID: "self-net"}, NetworkScope{Mode: "none"}); err == nil {
		t.Fatal("supervisor network namespace relabelled as none")
	}
}

// A real handed-off CONNECT stream must die on helper endpoint loss, while the
// execution context and long-lived policy remain otherwise live.
func TestFrontendLossRevokesHandedOffCONNECT(t *testing.T) {
	s := validNetworkTestScope()
	s.ExpiresAt = time.Now().Add(20 * time.Second).Unix()
	s.ScopeDigest = testDigest(s)
	g, err := NewGateway(context.Background(), "peer-loss", s)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	upstream, canary := net.Pipe()
	defer canary.Close()
	g.dial = func(context.Context, string, string) (net.Conn, error) { return upstream, nil }
	parent, frontend, err := NewChannel()
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	child, err := unixChannel(frontend)
	frontend.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	ready, done := make(chan error, 1), make(chan error, 1)
	go func() { done <- g.ServeChannel(parent, ready) }()
	if _, _, err = child.WriteMsgUnix([]byte{1}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err = <-ready; err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.SetDeadline(time.Now().Add(3 * time.Second))
	accepted, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	fd, err := accepted.(*net.TCPConn).File()
	accepted.Close()
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = child.WriteMsgUnix([]byte{2}, unix.UnixRights(int(fd.Fd())), nil)
	fd.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.WriteString(client, "CONNECT canary.example:443 HTTP/1.1\r\nHost: canary.example:443\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(client)
	response, err := http.ReadResponse(reader, nil)
	if err != nil || response.StatusCode != 200 {
		t.Fatalf("CONNECT positive control: %v %v", response, err)
	}
	echo := make(chan error, 1)
	go func() {
		b := make([]byte, 4)
		_, err := io.ReadFull(canary, b)
		if err == nil && string(b) != "ping" {
			err = fmt.Errorf("wrong canary data")
		}
		if err == nil {
			_, err = canary.Write([]byte("pong"))
		}
		echo <- err
	}()
	if _, err = client.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 4)
	if _, err = io.ReadFull(reader, b); err != nil || string(b) != "pong" {
		t.Fatalf("live stream positive: %q %v", b, err)
	}
	if err = <-echo; err != nil {
		t.Fatal(err)
	}
	child.Close() // no context cancellation, gateway.Close or expiry
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("frontend loss left broker and handed-off stream live")
	}
	if receipt := g.Receipt(); !receipt.Revoked || receipt.RevocationReason != "channel_lost" {
		t.Fatalf("peer loss did not revoke with distinct cause: %+v", receipt)
	}
	client.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	if _, err = reader.ReadByte(); err == nil {
		t.Fatal("client stream survived peer loss")
	} else if n, ok := err.(net.Error); ok && n.Timeout() {
		t.Fatal("client stream not closed")
	}
	canary.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	if _, err = canary.Read(b); err == nil {
		t.Fatal("upstream stream survived peer loss")
	} else if n, ok := err.(net.Error); ok && n.Timeout() {
		t.Fatal("upstream stream not closed")
	}
}

func TestChannelRejectsTruncatedSeqpacket(t *testing.T) {
	for _, ancillary := range []bool{false, true} {
		g, err := NewGateway(context.Background(), "truncated", validNetworkTestScope())
		if err != nil {
			t.Fatal(err)
		}
		parent, child := channelPair(t)
		ready, done := make(chan error, 1), make(chan error, 1)
		go func() { done <- g.ServeChannel(parent, ready) }()
		if _, _, err = child.WriteMsgUnix([]byte{1}, nil, nil); err != nil {
			t.Fatal(err)
		}
		if err = <-ready; err != nil {
			t.Fatal(err)
		}
		data, rights := []byte{2, 2, 2}, []byte(nil)
		if ancillary {
			f, err := os.Open("/dev/null")
			if err != nil {
				t.Fatal(err)
			}
			fds := make([]int, 32)
			for i := range fds {
				fds[i] = int(f.Fd())
			}
			rights = unix.UnixRights(fds...)
			data = []byte{2}
			_, _, err = child.WriteMsgUnix(data, rights, nil)
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
		} else if _, _, err = child.WriteMsgUnix(data, rights, nil); err != nil {
			t.Fatal(err)
		}
		select {
		case err = <-done:
			if err == nil {
				t.Fatal("truncation accepted")
			}
		case <-time.After(time.Second):
			g.Close()
			t.Fatal("truncation did not revoke")
		}
		if !g.Receipt().Revoked {
			t.Fatal("truncated channel stayed live")
		}
		g.Close()
	}
}
