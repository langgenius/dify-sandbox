package network

import "fmt"

// NetworkScope is a description, never a grant. Scoped networking is implemented
// by the network owner; the initial supervisor only supports an empty none scope.
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

func ValidateScope(s NetworkScope) error {
	if s.Mode != "none" || len(s.Destinations) != 0 || s.MaxEgressBytes != 0 || s.ExpiresAt != 0 || s.ScopeDigest != "" || s.InputManifestDigest != "" {
		return fmt.Errorf("network_scope: only empty mode=none is supported")
	}
	return nil
}
