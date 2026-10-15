package network

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"time"
)

// NetworkScope describes an immutable disclosure, never authority to mint a grant.
// Ordered destinations and addresses are part of its canonical JSON digest.
type NetworkScope struct {
	Mode                string        `json:"mode"`
	Destinations        []Destination `json:"destinations,omitempty"`
	InputManifestDigest string        `json:"input_manifest_digest,omitempty"`
	MaxEgressBytes      int64         `json:"max_egress_bytes,omitempty"`
	ExpiresAt           int64         `json:"expires_at,omitempty"`
	ScopeDigest         string        `json:"scope_digest,omitempty"`
}
type Destination struct {
	Protocol  string   `json:"protocol"`
	Host      string   `json:"host"`
	Port      uint16   `json:"port"`
	Addresses []string `json:"addresses"`
}

var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var hostLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func ScopeDigest(s NetworkScope) string {
	s.ScopeDigest = ""
	b, _ := json.Marshal(s)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func ValidateScope(s NetworkScope) error {
	if s.Mode == "none" {
		if len(s.Destinations) != 0 || s.InputManifestDigest != "" || s.MaxEgressBytes != 0 || s.ExpiresAt != 0 || s.ScopeDigest != "" {
			return fmt.Errorf("network_scope: none must be empty")
		}
		return nil
	}
	if s.Mode != "scoped" {
		return fmt.Errorf("network_scope: unknown mode")
	}
	if !digestPattern.MatchString(s.InputManifestDigest) || !digestPattern.MatchString(s.ScopeDigest) || s.ScopeDigest != ScopeDigest(s) {
		return fmt.Errorf("network_scope: invalid data or scope digest")
	}
	if s.MaxEgressBytes <= 0 || s.MaxEgressBytes > 1<<40 || s.ExpiresAt <= time.Now().Unix() || s.ExpiresAt > 253402300799 {
		return fmt.Errorf("network_scope: finite live byte/deadline limits required")
	}
	if len(s.Destinations) == 0 || len(s.Destinations) > 64 {
		return fmt.Errorf("network_scope: bounded destinations required")
	}
	seen := map[string]bool{}
	for _, d := range s.Destinations {
		switch d.Protocol {
		case "http", "https", "tcp", "udp", "dns":
		default:
			return fmt.Errorf("network_scope: unknown protocol")
		}
		if !validHost(d.Host) || d.Port == 0 || len(d.Addresses) == 0 || len(d.Addresses) > 16 {
			return fmt.Errorf("network_scope: exact host, port and pinned addresses required")
		}
		key := fmt.Sprintf("%s/%s/%d", d.Protocol, d.Host, d.Port)
		if seen[key] {
			return fmt.Errorf("network_scope: duplicate destination")
		}
		seen[key] = true
		addresses := map[string]bool{}
		for _, raw := range d.Addresses {
			ip, err := netip.ParseAddr(raw)
			if err != nil || !publicAddress(ip) || ip.String() != raw || addresses[raw] {
				return fmt.Errorf("network_scope: forbidden or ambiguous address")
			}
			addresses[raw] = true
		}
		if ip, err := netip.ParseAddr(d.Host); err == nil && (len(d.Addresses) != 1 || d.Addresses[0] != ip.String()) {
			return fmt.Errorf("network_scope: literal host differs from pinned address")
		}
	}
	return nil
}
func validHost(host string) bool {
	if ip, err := netip.ParseAddr(host); err == nil {
		return publicAddress(ip) && ip.String() == host
	}
	if len(host) > 253 || !strings.Contains(host, ".") || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if !hostLabel.MatchString(label) {
			return false
		}
	}
	return true
}
func publicAddress(ip netip.Addr) bool {
	if !ip.IsValid() || ip.Zone() != "" || ip.Is4In6() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	// Shared, transition and special-use routes can reach host/platform services
	// even when IsGlobalUnicast returns true. Documentation prefixes remain usable
	// as inert synthetic scope fixtures; they are never dialed by validation.
	for _, raw := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15", "240.0.0.0/4", "64:ff9b::/96", "64:ff9b:1::/48", "2001::/32", "2002::/16"} {
		if netip.MustParsePrefix(raw).Contains(ip) {
			return false
		}
	}
	return true
}

// SupportsHTTP describes syntax only. Profile qualification is a separate,
// server-owned prerequisite; raw transports must never fall back to this proxy.
func SupportsHTTP(s NetworkScope) bool {
	if ValidateScope(s) != nil {
		return false
	}
	if s.Mode == "none" {
		return true
	}
	for _, d := range s.Destinations {
		if d.Protocol != "http" && d.Protocol != "https" {
			return false
		}
	}
	return true
}
