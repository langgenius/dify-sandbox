package network

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGatewayRejectsAlternateTargetsBeforeDial(t *testing.T) {
	s := validNetworkTestScope()
	g, err := NewGateway(context.Background(), "exec-one", s)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	for _, target := range []string{"http://canary.example/", "https://other.example/", "https://canary.example:444/", "http://127.0.0.1/", "http://[::1]/", "http://169.254.169.254/"} {
		r := httptest.NewRequest("GET", target, nil)
		w := httptest.NewRecorder()
		g.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("alternate target accepted %s: %d", target, w.Code)
		}
	}
}

// A bounded, approved target must reach the gateway's explicit DNS validation,
// rather than passing solely because the implementation denies every request.
func TestGatewayApprovedTargetReachesPinnedDial(t *testing.T) {
	s := validNetworkTestScope()
	s.Destinations[0].Protocol = "http"
	s.Destinations[0].Port = 80
	s.ScopeDigest = testDigest(s)
	g, err := NewGateway(context.Background(), "exec-one", s)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	w := httptest.NewRecorder()
	g.ServeHTTP(w, httptest.NewRequest("GET", "http://canary.example/", nil))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("approved target did not reach pinned dial: %d", w.Code)
	}
}

func TestGatewayPinnedCanaryAndRedirect(t *testing.T) {
	s := validNetworkTestScope()
	s.Destinations[0].Protocol = "http"
	s.Destinations[0].Port = 8080
	s.MaxEgressBytes = 4096
	s.ScopeDigest = testDigest(s)
	g, err := NewGateway(context.Background(), "exec-one", s)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	requests := make(chan *http.Request, 2)
	g.dial = func(_ context.Context, protocol, address string) (net.Conn, error) {
		if protocol != "tcp" || address != "192.0.2.10:8080" {
			t.Errorf("unpinned dial %s %s", protocol, address)
		}
		a, b := net.Pipe()
		go func() {
			defer b.Close()
			r, err := http.ReadRequest(bufio.NewReader(b))
			if err != nil {
				return
			}
			io.Copy(io.Discard, r.Body)
			requests <- r
			io.WriteString(b, "HTTP/1.1 302 Found\r\nLocation: http://other.example/\r\nContent-Length: 6\r\n\r\ncanary")
		}()
		return a, nil
	}
	r := httptest.NewRequest("POST", "http://canary.example:8080/path", strings.NewReader("synthetic"))
	r.Header.Set("Proxy-Authorization", "must-not-leave")
	r.Header.Set("Connection", "X-Remove")
	r.Header.Set("X-Remove", "hop")
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != 302 || w.Body.String() != "canary" {
		t.Fatalf("approved canary failed: %d %s", w.Code, w.Body.String())
	}
	observed := <-requests
	if observed.Header.Get("Proxy-Authorization") != "" || observed.Header.Get("X-Remove") != "" {
		t.Fatal("proxy control headers escaped")
	}
	if g.Receipt().EgressBytes <= int64(len("synthetic")) {
		t.Fatal("quota omitted outbound HTTP headers")
	}
	location := w.Header().Get("Location")
	w = httptest.NewRecorder()
	g.ServeHTTP(w, httptest.NewRequest("GET", location, nil))
	if w.Code != 403 {
		t.Fatal("redirect target bypassed scope")
	}

}
func TestGatewayQuotaSharedAndRevocation(t *testing.T) {
	s := validNetworkTestScope()
	s.MaxEgressBytes = 4
	s.ScopeDigest = testDigest(s)
	g, err := NewGateway(context.Background(), "exec-quota", s)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	a, b := net.Pipe()
	defer b.Close()
	if err = g.track(a); err != nil {
		t.Fatal(err)
	}
	c := &egressConn{Conn: a, g: g}
	seen := make(chan string, 1)
	go func() { data := make([]byte, 4); n, _ := io.ReadFull(b, data); seen <- string(data[:n]) }()
	if n, err := c.Write([]byte("abc")); err != nil || n != 3 {
		t.Fatal(n, err)
	}
	if _, err := c.Write([]byte("de")); err == nil {
		t.Fatal("aggregate quota exceeded")
	}
	if got := <-seen; got != "abc" {
		t.Fatalf("quota leaked bytes %q", got)
	}
	if r := g.Receipt(); r.EgressBytes != 3 || !r.Revoked {
		t.Fatalf("wrong quota receipt %+v", r)
	}
}
func TestGatewayFreezesScopeAndCancellation(t *testing.T) {
	s := validNetworkTestScope()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g, err := NewGateway(ctx, "exec-freeze", s)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	s.Destinations[0].Addresses[0] = "192.0.2.11"
	s.Destinations[0].Host = "other.example"
	d, ok := g.destination("https", "canary.example", 443)
	if !ok || d.Addresses[0] != "192.0.2.10" {
		t.Fatal("caller mutated live gateway policy")
	}
	a, b := net.Pipe()
	defer b.Close()
	if err = g.track(a); err != nil {
		t.Fatal(err)
	}
	cancel()
	b.SetReadDeadline(time.Now().Add(time.Second))
	if _, err = b.Read(make([]byte, 1)); err == nil {
		t.Fatal("revocation kept active connection")
	}
	w := httptest.NewRecorder()
	g.ServeHTTP(w, httptest.NewRequest("GET", "http://canary.example/", nil))
	if w.Code != 403 {
		t.Fatal("revoked scope accepted")
	}
}

func TestAuthenticatedPinsNeverUseRuntimeDNS(t *testing.T) {
	old := net.DefaultResolver
	defer func() { net.DefaultResolver = old }()
	var dnsCalls atomic.Int64
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		dnsCalls.Add(1)
		return nil, fmt.Errorf("synthetic rebinding resolver must not be used")
	}}
	s := validNetworkTestScope()
	s.Destinations[0].Protocol = "http"
	s.Destinations[0].Port = 80
	s.ScopeDigest = testDigest(s)
	g, err := NewGateway(context.Background(), "no-runtime-dns", s)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	g.dial = func(_ context.Context, protocol, address string) (net.Conn, error) {
		if protocol != "tcp" || address != "192.0.2.10:80" {
			t.Errorf("changed resolver address used: %s", address)
		}
		a, b := net.Pipe()
		go func() {
			defer b.Close()
			if _, err := http.ReadRequest(bufio.NewReader(b)); err == nil {
				io.WriteString(b, "HTTP/1.1 200 OK\r\nContent-Length: 6\r\n\r\ncanary")
			}
		}()
		return a, nil
	}
	w := httptest.NewRecorder()
	g.ServeHTTP(w, httptest.NewRequest("GET", "http://canary.example/", nil))
	if w.Code != 200 || w.Body.String() != "canary" || dnsCalls.Load() != 0 {
		t.Fatalf("authenticated pins re-resolved: HTTP%d DNS calls%d", w.Code, dnsCalls.Load())
	}
}

func TestQuotaReceiptPreservesNonroutineCause(t *testing.T) {
	s := validNetworkTestScope()
	s.MaxEgressBytes = 128
	s.ScopeDigest = testDigest(s)
	g, err := NewGateway(context.Background(), "quota-cause", s)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	a, b := net.Pipe()
	defer b.Close()
	if err = g.track(a); err != nil {
		t.Fatal(err)
	}
	c := &egressConn{Conn: a, g: g}
	if _, err = c.Write(make([]byte, 4096)); err == nil {
		t.Fatal("oversize write accepted")
	}
	g.Close() // routine final close must not erase the enforcement cause
	receipt := g.Receipt()
	if receipt.RevocationReason != "egress_quota" || receipt.DeniedEgressBytes != 4096 || receipt.EgressBytes != 0 {
		t.Fatalf("quota indistinguishable from final close: %+v", receipt)
	}
}

func TestScopeExpiryReceiptSurvivesRoutineClose(t *testing.T) {
	s := validNetworkTestScope()
	s.ExpiresAt = time.Now().Add(time.Second).Unix()
	s.ScopeDigest = testDigest(s)
	g, err := NewGateway(context.Background(), "expiry-cause", s)
	if err != nil {
		t.Fatal(err)
	}
	<-g.ctx.Done()
	g.Close()
	if r := g.Receipt(); !r.Revoked || r.RevocationReason != "scope_expired" {
		t.Fatalf("expiry cause lost: %+v", r)
	}
}
