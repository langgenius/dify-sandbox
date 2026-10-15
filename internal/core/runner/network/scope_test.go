package network

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// testDigest is intentionally independent of the production digest helper.
func testDigest(s NetworkScope) string {
	s.ScopeDigest = ""
	b, _ := json.Marshal(s)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func validNetworkTestScope() NetworkScope {
	s := NetworkScope{Mode: "scoped", Destinations: []Destination{{Protocol: "https", Host: "canary.example", Port: 443, Addresses: []string{"192.0.2.10"}}}, InputManifestDigest: strings.Repeat("a", 64), MaxEgressBytes: 1024, ExpiresAt: time.Now().Add(time.Minute).Unix()}
	s.ScopeDigest = testDigest(s)
	return s
}
func TestScopedNetworkRequiresDataBinding(t *testing.T) {
	s := validNetworkTestScope()
	if err := ValidateScope(s); err != nil {
		t.Fatalf("valid bounded scope rejected: %v", err)
	}
	s.InputManifestDigest = ""
	s.ScopeDigest = testDigest(s)
	if err := ValidateScope(s); err == nil {
		t.Fatal("unbound data scope accepted")
	}
}
func TestScopeRejectsInvalidTargetsAndMutations(t *testing.T) {
	for name, mutate := range map[string]func(*NetworkScope){
		"unknown mode":     func(s *NetworkScope) { s.Mode = "all" },
		"expired":          func(s *NetworkScope) { s.ExpiresAt = time.Now().Unix() - 1 },
		"wildcard":         func(s *NetworkScope) { s.Destinations[0].Host = "*.example" },
		"no addresses":     func(s *NetworkScope) { s.Destinations[0].Addresses = nil },
		"zero port":        func(s *NetworkScope) { s.Destinations[0].Port = 0 },
		"unbounded bytes":  func(s *NetworkScope) { s.MaxEgressBytes = 0 },
		"unknown protocol": func(s *NetworkScope) { s.Destinations[0].Protocol = "socks" },
		"loopback":         func(s *NetworkScope) { s.Destinations[0].Addresses = []string{"127.0.0.1"} },
		"private":          func(s *NetworkScope) { s.Destinations[0].Addresses = []string{"10.0.0.1"} },
		"metadata":         func(s *NetworkScope) { s.Destinations[0].Addresses = []string{"169.254.169.254"} },
		"v6 loopback":      func(s *NetworkScope) { s.Destinations[0].Addresses = []string{"::1"} },
		"v6 private":       func(s *NetworkScope) { s.Destinations[0].Addresses = []string{"fd00::1"} },
		"v6 mapped":        func(s *NetworkScope) { s.Destinations[0].Addresses = []string{"::ffff:192.0.2.10"} },
		"v6 zone":          func(s *NetworkScope) { s.Destinations[0].Addresses = []string{"fe80::1%eth0"} },
		"proxy URL":        func(s *NetworkScope) { s.Destinations[0].Host = "http://proxy.example" },
		"digest format":    func(s *NetworkScope) { s.InputManifestDigest = "unbound" },
	} {
		t.Run(name, func(t *testing.T) {
			s := validNetworkTestScope()
			mutate(&s)
			s.ScopeDigest = testDigest(s)
			if ValidateScope(s) == nil {
				t.Fatal("invalid scope accepted")
			}
		})
	}
	for name, mutate := range map[string]func(*NetworkScope){
		"address":  func(s *NetworkScope) { s.Destinations[0].Addresses[0] = "192.0.2.11" },
		"port":     func(s *NetworkScope) { s.Destinations[0].Port = 444 },
		"protocol": func(s *NetworkScope) { s.Destinations[0].Protocol = "tcp" },
		"input":    func(s *NetworkScope) { s.InputManifestDigest = strings.Repeat("b", 64) },
		"bytes":    func(s *NetworkScope) { s.MaxEgressBytes++ },
		"expiry":   func(s *NetworkScope) { s.ExpiresAt++ },
	} {
		t.Run("mutated "+name, func(t *testing.T) {
			s := validNetworkTestScope()
			mutate(&s)
			if ValidateScope(s) == nil {
				t.Fatal("digest mutation accepted")
			}
		})
	}
}
func TestNoneIsEmptyAndDefaultDeny(t *testing.T) {
	if err := ValidateScope(NetworkScope{Mode: "none"}); err != nil {
		t.Fatal(err)
	}
	for _, s := range []NetworkScope{{}, {Mode: "none", Destinations: validNetworkTestScope().Destinations}, {Mode: "none", MaxEgressBytes: 1}, {Mode: "none", ScopeDigest: strings.Repeat("a", 64)}} {
		if ValidateScope(s) == nil {
			t.Fatal("ambiguous none accepted")
		}
	}
}
