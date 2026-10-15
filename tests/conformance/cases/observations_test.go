package cases

import (
	"github.com/langgenius/dify-sandbox/internal/core/runner/isolation"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOverlapEvidenceRequiresIntersectingUserIntervalsAndLiveCgroup(t *testing.T) {
	a := Evidence{Observation: isolation.Observation{ExecutionID: "a", StartedNanos: 10, ExitedNanos: 100}, ReadyNanos: 20}
	b := Evidence{Observation: isolation.Observation{ExecutionID: "b", StartedNanos: 15, ExitedNanos: 90}, ReadyNanos: 30, LivePeersAtReady: map[string]int64{"a": 30}}
	if err := requireOverlap(a, b); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Evidence){func(e *Evidence) { e.LivePeersAtReady = nil }, func(e *Evidence) { e.ReadyNanos = 110; e.ExitedNanos = 200 }, func(e *Evidence) { e.LivePeersAtReady = map[string]int64{"a": 100} }} {
		bad := b
		change(&bad)
		if requireOverlap(a, bad) == nil {
			t.Fatal("unproven overlap accepted")
		}
	}
}
func TestPeerSpanRequiresReadinessPostExitLivenessAndHeartbeat(t *testing.T) {
	offender := Evidence{Observation: isolation.Observation{ExecutionID: "bad", StartedNanos: 30, ExitedNanos: 60}, LivePeersAtExit: map[string]int64{"peer": 70}}
	peer := Evidence{Observation: isolation.Observation{ExecutionID: "peer", StartedNanos: 10, ExitedNanos: 100}, ReadyNanos: 20, LastAliveNanos: 80}
	if err := requirePeerSpan(offender, peer); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Evidence){func(e *Evidence) { e.ReadyNanos = 40 }, func(e *Evidence) { e.ExitedNanos = 50 }, func(e *Evidence) { e.LastAliveNanos = 65 }} {
		bad := peer
		change(&bad)
		if requirePeerSpan(offender, bad) == nil {
			t.Fatal("peer that did not span exhaustion accepted")
		}
	}
	offender.LivePeersAtExit = nil
	if requirePeerSpan(offender, peer) == nil {
		t.Fatal("missing kernel liveness accepted")
	}
}

// Real control-file reads test the recorder boundary. Synthetic files do not
// constitute cgroup enforcement evidence; that still requires the actual image.
func TestRecorderBoundsMarkersAndRequiresPopulatedPeer(t *testing.T) {
	dir := t.TempDir()
	cgroup := t.TempDir()
	events := filepath.Join(cgroup, "cgroup.events")
	if err := os.WriteFile(events, []byte("populated 1\nfrozen 0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	r := NewObservationRecorder(dir, 3)
	observe := func(o isolation.Observation) {
		t.Helper()
		if err := r.Observe(o); err != nil {
			t.Fatal(err)
		}
	}
	observe(isolation.Observation{ExecutionID: "peer", Phase: "running", Cgroup: cgroup, StartedNanos: 1})
	observe(isolation.Observation{ExecutionID: "peer", Phase: "stdout", ObservedNanos: 10, StdoutChunk: "CS1_RE"})
	observe(isolation.Observation{ExecutionID: "peer", Phase: "stdout", ObservedNanos: 20, StdoutChunk: "ADY\n"})
	observe(isolation.Observation{ExecutionID: "other", Phase: "running", Cgroup: cgroup, StartedNanos: 25})
	observe(isolation.Observation{ExecutionID: "other", Phase: "stdout", ObservedNanos: 30, StdoutChunk: strings.Repeat("x", 8192)})
	if len(r.entries["other"].line) > 64 {
		t.Fatal("unbounded diagnostic line")
	}
	observe(isolation.Observation{ExecutionID: "other", Phase: "stdout", ObservedNanos: 35, StdoutChunk: "CS1_READY\n"})
	if r.entries["other"].evidence.ReadyNanos != 0 {
		t.Fatal("suffix of oversized line forged readiness")
	}
	observe(isolation.Observation{ExecutionID: "other", Phase: "stdout", ObservedNanos: 40, StdoutChunk: "CS1_READY\n"})
	evidence, err := readEvidence(dir, "other")
	if err != nil {
		t.Fatal(err)
	}
	if evidence.ReadyNanos != 40 || evidence.LivePeersAtReady["peer"] != 40 {
		t.Fatal("ready peer kernel sample missing")
	}
	observe(isolation.Observation{ExecutionID: "other", Phase: "completed", StartedNanos: 25, ExitedNanos: 50, ObservedNanos: 60})
	evidence, err = readEvidence(dir, "other")
	if err != nil {
		t.Fatal(err)
	}
	if evidence.LivePeersAtExit["peer"] != 60 {
		t.Fatal("post-exit live peer not sampled")
	}
	if err = os.WriteFile(events, []byte("populated 0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	observe(isolation.Observation{ExecutionID: "last", Phase: "running", StartedNanos: 65})
	observe(isolation.Observation{ExecutionID: "last", Phase: "stdout", ObservedNanos: 70, StdoutChunk: "CS1_READY\n"})
	evidence, err = readEvidence(dir, "last")
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence.LivePeersAtReady) != 0 {
		t.Fatal("terminated peer treated as live")
	}
	if err = r.Observe(isolation.Observation{ExecutionID: "overflow", Phase: "running"}); err == nil {
		t.Fatal("unbounded observation registry")
	}
}

func TestRecorderRejectsBufferedMarkersFromExitedProcess(t *testing.T) {
	dir := t.TempDir()
	cgroup := t.TempDir()
	if err := os.WriteFile(filepath.Join(cgroup, "cgroup.events"), []byte("populated 0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	r := NewObservationRecorder(dir, 1)
	if err := r.Observe(isolation.Observation{ExecutionID: "dead", Phase: "running", Cgroup: cgroup, StartedNanos: 1}); err != nil {
		t.Fatal(err)
	}
	if err := r.Observe(isolation.Observation{ExecutionID: "dead", Phase: "stdout", ObservedNanos: 10, StdoutChunk: "CS1_READY\nCS1_ALIVE\n"}); err != nil {
		t.Fatal(err)
	}
	e, err := readEvidence(dir, "dead")
	if err != nil {
		t.Fatal(err)
	}
	if e.ReadyNanos != 0 || e.LastAliveNanos != 0 {
		t.Fatal("buffered output from dead process accepted as liveness")
	}
}
