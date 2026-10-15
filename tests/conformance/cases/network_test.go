package cases

import (
	"encoding/json"
	"github.com/langgenius/dify-sandbox/internal/core/runner/isolation"
	"github.com/langgenius/dify-sandbox/internal/core/runner/network"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNetworkEvidenceRequiresPositiveControlForEveryTarget(t *testing.T) {
	targets := []NetworkTarget{{Protocol: "tcp", Address: "192.0.2.10", Port: 8080}, {Protocol: "udp", Address: "2001:db8::10", Port: 8081}}
	before := NetworkCounters{BootID: "boot-one", Counters: map[string]uint64{targets[0].key(): 3, targets[1].key(): 8}}
	missing := NetworkCounters{BootID: "boot-one", Counters: map[string]uint64{targets[0].key(): 4, targets[1].key(): 8}}
	if compareNetwork(before, missing, targets, true) == nil {
		t.Fatal("unobserved UDP/v6 control accepted")
	}
	positive := NetworkCounters{BootID: "boot-one", Counters: map[string]uint64{targets[0].key(): 4, targets[1].key(): 9}}
	if err := compareNetwork(before, positive, targets, true); err != nil {
		t.Fatal(err)
	}
	if compareNetwork(before, positive, targets, false) == nil {
		t.Fatal("denied canary traffic accepted")
	}
	if err := compareNetwork(positive, positive, targets, false); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []NetworkCounters{{BootID: "restart", Counters: positive.Counters}, {BootID: "boot-one", Counters: map[string]uint64{targets[0].key(): 2, targets[1].key(): 8}}, {BootID: "boot-one", Counters: map[string]uint64{}}} {
		if compareNetwork(before, bad, targets, false) == nil {
			t.Fatal("unreliable counter evidence accepted")
		}
	}
}
func TestNetworkFixtureRequiresAllProtocolsAndFamilies(t *testing.T) {
	fixture := NetworkFixture{CollectorURL: "http://collector.fixture.test/counts"}
	if fixture.validate() == nil {
		t.Fatal("empty fixture accepted")
	}
	for _, protocol := range []string{"tcp", "udp", "dns", "http", "https"} {
		for _, address := range []string{"192.0.2.10", "2001:db8::10"} {
			fixture.Targets = append(fixture.Targets, NetworkTarget{Protocol: protocol, Address: address, Host: address, Port: 8080})
		}
	}
	for _, protocol := range []string{"http", "https"} {
		for _, address := range []string{"192.0.2.11", "2001:db8::11"} {
			fixture.AlternateTargets = append(fixture.AlternateTargets, NetworkTarget{Protocol: protocol, Address: address, Host: address, Port: 8080})
		}
	}
	if err := fixture.validate(); err != nil {
		t.Fatal(err)
	}
	fixture.Targets = fixture.Targets[:len(fixture.Targets)-1]
	if fixture.validate() == nil {
		t.Fatal("missing HTTPS/v6 positive control accepted")
	}
}

func TestQuotaEvidenceRejectsUnexercisedOrRoutineCompletion(t *testing.T) {
	target := NetworkTarget{Protocol: "http", Address: "192.0.2.10", Port: 80}
	before := NetworkCounters{BootID: "one", Counters: map[string]uint64{target.key(): 1}, ReceivedBytes: map[string]uint64{target.key(): 100}}
	after := NetworkCounters{BootID: "one", Counters: map[string]uint64{target.key(): 2}, ReceivedBytes: map[string]uint64{target.key(): 120}}
	positive := isolation.ExecutionResult{Status: "succeeded", Stdout: "QUOTA_ATTEMPT_4096\nQUOTA_EXERCISED\n", NetworkReceipt: &network.NetworkReceipt{EgressBytes: 20, Revoked: true, RevocationReason: "egress_quota", DeniedEgressBytes: 4096}}
	if err := quotaEvidence(positive, before, after, target, 128); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"pre-request-failure", "missing-marker", "routine-close", "no-rejected-write", "no-canary-connection", "over-ceiling", "missing-bytes", "regressed-bytes", "restarted"} {
		t.Run(name, func(t *testing.T) {
			r := positive
			receipt := *positive.NetworkReceipt
			r.NetworkReceipt = &receipt
			a := after
			switch name {
			case "pre-request-failure":
				r.Status = "failed"
			case "missing-marker":
				r.Stdout = ""
			case "routine-close":
				receipt.RevocationReason = "closed"
			case "no-rejected-write":
				receipt.DeniedEgressBytes = 0
			case "no-canary-connection":
				a.Counters = before.Counters
			case "over-ceiling":
				a.ReceivedBytes = map[string]uint64{target.key(): 229}
			case "missing-bytes":
				a.ReceivedBytes = nil
			case "regressed-bytes":
				a.ReceivedBytes = map[string]uint64{target.key(): 99}
			case "restarted":
				a.BootID = "two"
			}
			if quotaEvidence(r, before, a, target, 128) == nil {
				t.Fatal("invalid quota evidence accepted")
			}
		})
	}
}
func TestExpiryEvidenceRejectsSetupAndWallTimeoutFalsePositives(t *testing.T) {
	result := isolation.ExecutionResult{Status: "failed", FailureReason: "deadline"}
	if err := expiryEvidence(result, true, true, time.Second, 3500*time.Millisecond, 3*time.Second, time.Second, 8*time.Second); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name                   string
		ready, live            bool
		liveAt, finished, wall time.Duration
	}{
		{"setup-timeout", false, false, 0, 3 * time.Second, 8 * time.Second},
		{"no-live-hold", true, false, time.Second, 3 * time.Second, 8 * time.Second},
		{"late-live", true, true, 3 * time.Second, 3500 * time.Millisecond, 8 * time.Second},
		{"wall-only", true, true, time.Second, 8 * time.Second, 8 * time.Second},
		{"indistinguishable-wall", true, true, time.Second, 3500 * time.Millisecond, 4 * time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			if expiryEvidence(result, test.ready, test.live, test.liveAt, test.finished, 3*time.Second, time.Second, test.wall) == nil {
				t.Fatal("false expiry evidence accepted")
			}
		})
	}
}

func TestCollectorRequiresReceivedBytesForEveryTarget(t *testing.T) {
	target := NetworkTarget{Protocol: "http", Address: "192.0.2.10", Host: "192.0.2.10", Port: 19004}
	data := NetworkCounters{BootID: "real-boot", Counters: map[string]uint64{target.key(): 1}, Active: map[string]uint64{target.key(): 0}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(data) }))
	defer server.Close()
	fixture := NetworkFixture{CollectorURL: server.URL, Targets: []NetworkTarget{target}}
	if _, err := collectNetwork(server.Client(), fixture); err == nil {
		t.Fatal("missing received_bytes accepted")
	}
	data.ReceivedBytes = map[string]uint64{target.key(): 42}
	if _, err := collectNetwork(server.Client(), fixture); err != nil {
		t.Fatal(err)
	}
}
