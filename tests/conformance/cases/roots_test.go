package cases

import (
	"encoding/json"
	"github.com/langgenius/dify-sandbox/internal/core/runner/isolation"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// This is an intentionally serial candidate protocol fixture. It tests the
// harness's rejection of supplied evidence, not kernel isolation qualification.
func TestRootsAndBudgetsRejectsNonConcurrentEvidence(t *testing.T) {
	for _, scenario := range []string{"advertised_single_worker", "serialized_roots", "peer_finished_early"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			obs := filepath.Join(dir, "observations")
			if err := os.Mkdir(obs, 0700); err != nil {
				t.Fatal(err)
			}
			b := isolation.Budget{InputBytes: 8192, CodeBytes: 4096, StdoutBytes: 8192, StderrBytes: 1024, DiskBytes: 1 << 20, MemoryBytes: 64 << 20, PIDs: 32, FDs: 32, Concurrent: 2, QueueDepth: 2, CPUQuotaMicros: 10000, CPUPeriodMicros: 100000, CPUTimeMillis: 100, WallMillis: 2000, QueueMillis: 100, CleanupMillis: 100}
			p := isolation.Profile{Digest: strings.Repeat("a", 64), EnvironmentDigest: strings.Repeat("b", 64), EnvironmentRoot: filepath.Join(dir, "env"), WorkRoot: filepath.Join(dir, "work"), CgroupRoot: filepath.Join(dir, "cgroup"), WorkerPath: "/worker", Languages: []string{"python3", "nodejs"}, Limits: b, MaxConcurrent: 2, MaxQueueDepth: 2, MaxQueuedBytes: 32768, MaxExecutions: 30}
			if scenario == "advertised_single_worker" {
				p.MaxConcurrent = 1
			}
			profile := filepath.Join(dir, "profile.json")
			raw, _ := json.Marshal(p)
			if err := os.WriteFile(profile, raw, 0600); err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				var req isolation.InvocationRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				id := req.Options.ExecutionID
				result := isolation.ExecutionResult{ExecutionID: id, InvocationDigest: req.Options.InvocationDigest, Status: "succeeded", CleanupState: "clean", ExitCode: 0, Stdout: "/work"}
				controls := map[string]string{"memory.max": strconv.FormatInt(b.MemoryBytes, 10), "memory.swap.max": "0", "memory.oom.group": "1", "pids.max": strconv.FormatInt(b.PIDs, 10), "cpu.max": "10000 100000", "memory.events": "oom_kill 1", "pids.events": "max 1"}
				if strings.HasPrefix(id, "cs1-limit-") {
					result.Status = "failed"
					result.ExitCode = 1
					result.FailureReason = "cpu_limit"
					result.Stderr = "FD_LIMIT:24 DISK_LIMIT:28"
				}
				if strings.HasPrefix(id, "cs1-peer-") {
					result.Stdout = "peer-alive"
				}
				if id == "cs1-serial" {
					result.Stdout = "isolated"
				}
				namespaces := map[string]string{}
				for _, ns := range []string{"mnt", "pid", "ipc", "uts", "net"} {
					namespaces[ns] = id + "-" + ns
				}
				evidence := map[string]any{"execution_id": id, "phase": "completed", "root": id, "cgroup": id, "namespaces": namespaces, "controls": controls, "root_removed": true, "cgroup_removed": true, "result": result, "started_nanos": int64(10), "exited_nanos": int64(500), "ready_nanos": int64(20), "last_alive_nanos": int64(450), "live_peers_at_ready": map[string]int64{}, "live_peers_at_exit": map[string]int64{}}
				if id == "cs1-roots-1" {
					if scenario == "peer_finished_early" {
						evidence["live_peers_at_ready"] = map[string]int64{"cs1-roots-0": 30}
					} else {
						evidence["started_nanos"] = int64(600)
						evidence["ready_nanos"] = int64(610)
						evidence["exited_nanos"] = int64(700)
					}
				}
				if strings.HasPrefix(id, "cs1-peer-") {
					evidence["ready_nanos"] = int64(1000)
					evidence["exited_nanos"] = int64(1100)
					evidence["last_alive_nanos"] = int64(1050)
				}
				if strings.HasPrefix(id, "cs1-limit-") {
					evidence["started_nanos"] = int64(1200)
					evidence["exited_nanos"] = int64(1400)
				}
				raw, _ := json.Marshal(evidence)
				if err := os.WriteFile(filepath.Join(obs, id+".json"), raw, 0600); err != nil {
					t.Error(err)
				}
				json.NewEncoder(w).Encode(result)
			}))
			defer server.Close()
			if err := RootsAndBudgets(server.URL, profile, obs); err == nil {
				t.Fatal("non-concurrent candidate falsely qualified")
			}
		})
	}
}
