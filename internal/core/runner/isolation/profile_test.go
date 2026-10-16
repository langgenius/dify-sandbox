package isolation

import (
	"bytes"
	"github.com/langgenius/dify-sandbox/internal/core/runner/environment"
	"github.com/langgenius/dify-sandbox/internal/core/runner/network"
	"math"
	"strings"
	"testing"
)

func validTestBudget() Budget {
	return Budget{InputBytes: 8192, CodeBytes: 4096, StdoutBytes: 1024, StderrBytes: 1024, DiskBytes: 1 << 20, MemoryBytes: 64 << 20, PIDs: 32, FDs: 32, Concurrent: 1, QueueDepth: 2, CPUQuotaMicros: 10000, CPUPeriodMicros: 100000, CPUTimeMillis: 1000, WallMillis: 2000, QueueMillis: 100, CleanupMillis: 100}
}
func TestRejectsInvalidBudget(t *testing.T) {
	for _, mutate := range []func(*Budget){func(b *Budget) { b.MemoryBytes = 0 }, func(b *Budget) { b.QueueMillis = -1 }, func(b *Budget) { b.WallMillis = math.MaxInt64 }, func(b *Budget) { b.InputBytes = math.MaxInt64 }, func(b *Budget) { b.CPUPeriodMicros = 999 }} {
		b := validTestBudget()
		mutate(&b)
		if b.Validate() == nil {
			t.Fatal("invalid budget admitted")
		}
	}
	if err := validTestBudget().Validate(); err != nil {
		t.Fatal(err)
	}
}
func testProfile(t *testing.T) Profile {
	t.Helper()
	root := t.TempDir()
	m, err := environment.BuildManifest(root, environment.EnvironmentManifest{PythonVersion: "fixture", NodeVersion: "fixture", JinjaVersion: "fixture", Architecture: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	if err = environment.WriteManifest(root, m); err != nil {
		t.Fatal(err)
	}
	return Profile{Digest: strings.Repeat("a", 64), EnvironmentDigest: m.Digest, EnvironmentRoot: root, WorkRoot: t.TempDir(), CgroupRoot: t.TempDir(), WorkerPath: "/request-worker", Languages: []string{"python3", "nodejs"}, Limits: validTestBudget(), MaxConcurrent: 1, MaxQueueDepth: 2, MaxQueuedBytes: 32768, MaxExecutions: 20, HostUIDMin: 21000, HostUIDMax: 21016}
}
func testSpec(p Profile) InvocationSpec {
	return InvocationSpec{Version: 1, ExecutionID: "request-1", Language: "python3", EnvironmentDigest: p.EnvironmentDigest, ProfileDigest: p.Digest, Budget: p.Limits, Network: network.NetworkScope{Mode: "none"}, BudgetReservationRef: "reservation-1"}
}
func TestEnvelopeRejectsUnknownAndUnboundedInput(t *testing.T) {
	for _, raw := range []string{`{"schema_version":1,"unexpected":1}`, `{"schema_version":9223372036854775808}`, `{} {}`} {
		if _, err := DecodeInvocation(strings.NewReader(raw), 8192); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if _, err := DecodeInvocation(bytes.NewReader(bytes.Repeat([]byte(" "), 8193)), 8192); err == nil {
		t.Fatal("oversize body admitted")
	}
}
func TestProfileAndInvocationAuthority(t *testing.T) {
	p := testProfile(t)
	s := testSpec(p)
	payload := []byte(`{"code":"print(1)","preload":""}`)
	s.InvocationDigest = InvocationDigest(s, payload)
	if err := p.ValidateInvocation(s, payload, p.Limits); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*InvocationSpec){func(s *InvocationSpec) { s.Language = "shell" }, func(s *InvocationSpec) { s.Budget.MemoryBytes++ }, func(s *InvocationSpec) { s.InvocationDigest = strings.Repeat("c", 64) }, func(s *InvocationSpec) { s.ProfileDigest = "other" }, func(s *InvocationSpec) { s.Network.Mode = "scoped" }, func(s *InvocationSpec) { s.BudgetReservationRef = "" }} {
		bad := s
		mutate(&bad)
		if p.ValidateInvocation(bad, payload, p.Limits) == nil {
			t.Fatal("invalid invocation admitted")
		}
	}
	reservation := p.Limits
	reservation.MemoryBytes--
	if p.ValidateInvocation(s, payload, reservation) == nil {
		t.Fatal("reservation exceeded")
	}
	p.MaxQueuedBytes = 0
	if p.Validate() == nil {
		t.Fatal("unbounded profile admitted")
	}
}
