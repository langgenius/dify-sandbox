package cases

import (
	"encoding/json"
	"fmt"
	"github.com/langgenius/dify-sandbox/internal/core/runner/isolation"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Evidence is private to the synthetic candidate/harness. Readiness comes from
// the fixed workload's bounded stdout, but liveness comes from delegated kernel
// cgroups, never from the workload's claim to still be alive.
type Evidence struct {
	isolation.Observation
	ReadyNanos       int64            `json:"ready_nanos"`
	LastAliveNanos   int64            `json:"last_alive_nanos"`
	LivePeersAtReady map[string]int64 `json:"live_peers_at_ready"`
	LivePeersAtExit  map[string]int64 `json:"live_peers_at_exit"`
}
type observationEntry struct {
	evidence Evidence
	line     []byte
	discard  bool
}
type ObservationRecorder struct {
	mu        sync.Mutex
	directory string
	limit     int64
	entries   map[string]*observationEntry
}

func NewObservationRecorder(directory string, limit int64) *ObservationRecorder {
	return &ObservationRecorder{directory: directory, limit: limit, entries: map[string]*observationEntry{}}
}
func (r *ObservationRecorder) Observe(o isolation.Observation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.entries[o.ExecutionID]
	if entry == nil {
		if int64(len(r.entries)) >= r.limit {
			return fmt.Errorf("observation capacity exhausted")
		}
		entry = &observationEntry{evidence: Evidence{LivePeersAtReady: map[string]int64{}, LivePeersAtExit: map[string]int64{}}}
		r.entries[o.ExecutionID] = entry
	}
	if o.Phase == "stdout" {
		// Parse only fixed short markers. Arbitrarily long output lines cannot grow
		// diagnostic memory and stdout beyond the execution budget is never observed.
		for _, b := range []byte(o.StdoutChunk) {
			if b == '\n' {
				if !entry.discard && cgroupPopulated(entry.evidence.Cgroup) {
					// Pipes can deliver buffered markers after their writer exits.
					// Accept markers only while the kernel still observes this group.
					switch string(entry.line) {
					case "CS1_READY":
						if entry.evidence.ReadyNanos == 0 {
							entry.evidence.ReadyNanos = o.ObservedNanos
							entry.evidence.LivePeersAtReady = r.livePeers(o.ExecutionID, o.ObservedNanos)
						}
					case "CS1_ALIVE":
						entry.evidence.LastAliveNanos = o.ObservedNanos
					}
				}
				entry.line = entry.line[:0]
				entry.discard = false
			} else if !entry.discard {
				if len(entry.line) >= 64 {
					entry.discard = true
					entry.line = entry.line[:0]
				} else {
					entry.line = append(entry.line, b)
				}
			}
		}
	} else {
		o.StdoutChunk = ""
		o.Controls = cloneStrings(o.Controls)
		o.Namespaces = cloneStrings(o.Namespaces)
		entry.evidence.Observation = o
		if o.Phase == "completed" && o.ExitedNanos > 0 {
			entry.evidence.LivePeersAtExit = r.livePeers(o.ExecutionID, o.ObservedNanos)
		}
	}
	data, err := json.Marshal(entry.evidence)
	if err != nil {
		return err
	}
	target := filepath.Join(r.directory, o.ExecutionID+".json")
	if err = os.WriteFile(target+".tmp", data, 0444); err != nil {
		return err
	}
	return os.Rename(target+".tmp", target)
}
func cloneStrings(source map[string]string) map[string]string {
	copy := make(map[string]string, len(source))
	for k, v := range source {
		copy[k] = v
	}
	return copy
}
func (r *ObservationRecorder) livePeers(exclude string, at int64) map[string]int64 {
	live := map[string]int64{}
	for id, entry := range r.entries {
		e := entry.evidence
		if id == exclude || e.ReadyNanos <= 0 || e.Phase == "completed" || e.Cgroup == "" {
			continue
		}
		if cgroupPopulated(e.Cgroup) {
			live[id] = at
		}
	}
	return live
}
func cgroupPopulated(directory string) bool {
	if directory == "" {
		return false
	}
	events, err := os.ReadFile(filepath.Join(directory, "cgroup.events"))
	return err == nil && positiveEvent(string(events), "populated")
}
func readEvidence(directory, id string) (Evidence, error) {
	var e Evidence
	b, err := os.ReadFile(filepath.Join(directory, id+".json"))
	if err != nil {
		return e, err
	}
	err = json.Unmarshal(b, &e)
	return e, err
}
func requireOverlap(a, b Evidence) error {
	start := max(a.ReadyNanos, b.ReadyNanos)
	end := min(a.ExitedNanos, b.ExitedNanos)
	if a.ReadyNanos <= 0 || b.ReadyNanos <= 0 || start >= end {
		return fmt.Errorf("mixed-language user execution did not overlap")
	}
	for _, at := range []int64{a.LivePeersAtReady[b.ExecutionID], b.LivePeersAtReady[a.ExecutionID]} {
		if at >= start && at < end {
			return nil
		}
	}
	return fmt.Errorf("no independent live-peer cgroup observation during mixed-language overlap")
}
func requirePeerSpan(offender, peer Evidence) error {
	checked := offender.LivePeersAtExit[peer.ExecutionID]
	if peer.ReadyNanos <= 0 || peer.ReadyNanos >= offender.StartedNanos || offender.StartedNanos >= offender.ExitedNanos || checked <= offender.ExitedNanos || checked >= peer.ExitedNanos || peer.LastAliveNanos <= checked || peer.LastAliveNanos >= peer.ExitedNanos {
		return fmt.Errorf("peer did not remain active across %s exhaustion", offender.ExecutionID)
	}
	return nil
}
func workloadOutput(stdout string) string {
	lines := []string{}
	for _, line := range strings.Split(stdout, "\n") {
		if line != "CS1_READY" && line != "CS1_ALIVE" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}
