//go:build linux

package network

import (
	"bufio"
	"context"
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
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	parent := os.NewFile(uintptr(fds[0]), "parent")
	child := os.NewFile(uintptr(fds[1]), "child")
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
