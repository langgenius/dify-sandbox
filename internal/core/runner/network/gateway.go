package network

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrCapability = errors.New("network_capability: qualified HTTP mediation required")

type NetworkReceipt struct {
	ExecutionID  string `json:"execution_id"`
	PolicyDigest string `json:"policy_digest"`
	EgressBytes  int64  `json:"egress_bytes"`
	Revoked      bool   `json:"revoked"`
}

// Gateway owns one authenticated execution policy. A private inherited channel
// supplies connected client FDs, never rules. No rule-management endpoint exists.
type Gateway struct {
	scope        NetworkScope
	executionID  string
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.Mutex
	connections  map[net.Conn]struct{}
	closed       bool
	channelReady bool
	budgetMu     sync.Mutex
	used         int64
	active       chan struct{}
	dial         func(context.Context, string, string) (net.Conn, error)
}

func NewGateway(ctx context.Context, executionID string, scope NetworkScope) (*Gateway, error) {
	if err := ValidateScope(scope); err != nil {
		return nil, err
	}
	if executionID == "" || scope.Mode != "scoped" || !SupportsHTTP(scope) {
		return nil, ErrCapability
	}
	// Freeze caller-owned slices before asynchronous use.
	scope.Destinations = append([]Destination(nil), scope.Destinations...)
	for i := range scope.Destinations {
		scope.Destinations[i].Addresses = append([]string(nil), scope.Destinations[i].Addresses...)
	}
	ctx, cancel := context.WithDeadline(ctx, time.Unix(scope.ExpiresAt, 0))
	g := &Gateway{scope: scope, executionID: executionID, ctx: ctx, cancel: cancel, connections: map[net.Conn]struct{}{}, active: make(chan struct{}, 16)}
	g.dial = (&net.Dialer{Timeout: 5 * time.Second}).DialContext
	go func() { <-ctx.Done(); g.Close() }()
	return g, nil
}
func (g *Gateway) Receipt() NetworkReceipt {
	g.budgetMu.Lock()
	used := g.used
	g.budgetMu.Unlock()
	g.mu.Lock()
	closed := g.closed
	g.mu.Unlock()
	return NetworkReceipt{ExecutionID: g.executionID, PolicyDigest: g.scope.ScopeDigest, EgressBytes: used, Revoked: closed}
}
func (g *Gateway) Close() error {
	g.cancel()
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.closed {
		g.closed = true
		for c := range g.connections {
			c.Close()
		}
		g.connections = map[net.Conn]struct{}{}
	}
	return nil
}
func (g *Gateway) track(c net.Conn) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || g.ctx.Err() != nil {
		c.Close()
		return context.Canceled
	}
	if len(g.connections) >= 64 {
		c.Close()
		return fmt.Errorf("connection bound exceeded")
	}
	g.connections[c] = struct{}{}
	if deadline, ok := g.ctx.Deadline(); ok {
		c.SetDeadline(deadline)
	}
	return nil
}
func (g *Gateway) untrack(c net.Conn) {
	c.Close()
	g.mu.Lock()
	delete(g.connections, c)
	g.mu.Unlock()
}
func (g *Gateway) destination(protocol, host string, port uint16) (Destination, bool) {
	for _, d := range g.scope.Destinations {
		if d.Protocol == protocol && d.Host == host && d.Port == port {
			return d, true
		}
	}
	return Destination{}, false
}
func target(r *http.Request) (string, string, uint16, error) {
	protocol, host, port := "", "", uint64(0)
	if r.Method == http.MethodConnect {
		protocol = "https"
		var raw string
		var err error
		host, raw, err = net.SplitHostPort(r.Host)
		if err != nil {
			return "", "", 0, err
		}
		port, err = strconv.ParseUint(raw, 10, 16)
		if err != nil {
			return "", "", 0, err
		}
		if r.URL.Host != r.Host || r.URL.Path != "" || r.URL.RawQuery != "" {
			return "", "", 0, fmt.Errorf("ambiguous CONNECT")
		}
	} else {
		if r.URL.Scheme != "http" || r.URL.User != nil || r.URL.Fragment != "" || r.Host != r.URL.Host {
			return "", "", 0, fmt.Errorf("absolute HTTP target required")
		}
		protocol = "http"
		host = r.URL.Hostname()
		port = 80
		if raw := r.URL.Port(); raw != "" {
			var err error
			port, err = strconv.ParseUint(raw, 10, 16)
			if err != nil {
				return "", "", 0, err
			}
		}
	}
	if !validHost(host) || port == 0 {
		return "", "", 0, fmt.Errorf("invalid exact target")
	}
	return protocol, host, uint16(port), nil
}
func (g *Gateway) dialPinned(ctx context.Context, d Destination) (net.Conn, error) {
	if err := g.ctx.Err(); err != nil {
		return nil, err
	}
	// DNS qualification belongs to the authenticated metadata owner. No runtime
	// lookup can change its approved pins or emit undisclosed resolver traffic.
	c, err := g.dial(ctx, "tcp", net.JoinHostPort(d.Addresses[0], strconv.Itoa(int(d.Port))))
	if err != nil {
		return nil, err
	}
	if err = g.track(c); err != nil {
		return nil, err
	}
	return &egressConn{Conn: c, g: g}, nil
}

type egressConn struct {
	net.Conn
	g *Gateway
}

func (c *egressConn) Write(p []byte) (int, error) {
	g := c.g
	g.budgetMu.Lock()
	if g.ctx.Err() != nil {
		g.budgetMu.Unlock()
		return 0, context.Canceled
	}
	remaining := g.scope.MaxEgressBytes - g.used
	if int64(len(p)) > remaining {
		g.budgetMu.Unlock()
		g.Close()
		return 0, fmt.Errorf("network egress quota exceeded")
	}
	// Reserve before writing; failed/partial writes conservatively consume the
	// reservation, avoiding double spends across concurrent destinations.
	g.used += int64(len(p))
	g.budgetMu.Unlock()
	return c.Conn.Write(p)
}
func (c *egressConn) Close() error { c.g.untrack(c.Conn); return nil }
func stripHop(h http.Header) {
	for _, key := range strings.Split(h.Get("Connection"), ",") {
		h.Del(strings.TrimSpace(key))
	}
	for _, key := range []string{"Connection", "Proxy-Connection", "Proxy-Authorization", "Proxy-Authenticate", "Keep-Alive", "TE", "Trailer", "Transfer-Encoding", "Upgrade"} {
		h.Del(key)
	}
}
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if g.ctx.Err() != nil {
		http.Error(w, "scope expired or revoked", http.StatusForbidden)
		return
	}
	protocol, host, port, err := target(r)
	if err != nil {
		http.Error(w, "target denied", http.StatusForbidden)
		return
	}
	d, ok := g.destination(protocol, host, port)
	if !ok {
		http.Error(w, "target denied", http.StatusForbidden)
		return
	}
	select {
	case g.active <- struct{}{}:
		defer func() { <-g.active }()
	default:
		http.Error(w, "gateway capacity", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithCancel(g.ctx)
	defer cancel()
	stop := context.AfterFunc(r.Context(), cancel)
	defer stop()
	if r.Method == http.MethodConnect {
		g.connect(ctx, w, r, d)
		return
	}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, MaxResponseHeaderBytes: 16 << 10, ResponseHeaderTimeout: 5 * time.Second, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) { return g.dialPinned(ctx, d) }}
	defer transport.CloseIdleConnections()
	request := r.Clone(ctx)
	request.RequestURI = ""
	stripHop(request.Header)
	request.Close = true
	response, err := transport.RoundTrip(request)
	if err != nil {
		http.Error(w, "upstream denied or unavailable", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	// Redirects are returned to the client. Its subsequent request goes through
	// this same full validation; the transport never follows a Location itself.
	stripHop(response.Header)
	for k, v := range response.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(response.StatusCode)
	io.Copy(w, response.Body)
}
func (g *Gateway) connect(ctx context.Context, w http.ResponseWriter, r *http.Request, d Destination) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "tunnel unavailable", http.StatusBadGateway)
		return
	}
	upstream, err := g.dialPinned(ctx, d)
	if err != nil {
		http.Error(w, "upstream denied or unavailable", http.StatusBadGateway)
		return
	}
	defer upstream.Close()
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		return
	}
	if err = g.track(client); err != nil {
		return
	}
	defer g.untrack(client)
	if _, err = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	if err = buffered.Flush(); err != nil {
		return
	}
	done := make(chan struct{})
	go func() { io.Copy(upstream, buffered); upstream.Close(); client.Close(); close(done) }()
	io.Copy(client, upstream)
	client.Close()
	upstream.Close()
	<-done
}
