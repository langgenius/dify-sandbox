package cases

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/langgenius/dify-sandbox/internal/core/runner/isolation"
	"github.com/langgenius/dify-sandbox/internal/core/runner/network"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RootsAndBudgets requires a real candidate controller and independently written
// supervisor observations. Missing capabilities/evidence fail the case.
func RootsAndBudgets(endpoint, profilePath, observations string) error {
	raw, err := os.ReadFile(profilePath)
	if err != nil {
		return fmt.Errorf("qualified candidate profile required: %w", err)
	}
	var p isolation.Profile
	if err = json.Unmarshal(raw, &p); err != nil {
		return err
	}
	if err = p.Validate(); err != nil {
		return err
	}
	client := &http.Client{Timeout: time.Duration(p.Limits.WallMillis+p.Limits.QueueMillis+p.Limits.CleanupMillis+1000) * time.Millisecond}
	run := func(id, language, code string) (isolation.ExecutionResult, error) {
		spec := isolation.InvocationSpec{Version: 1, ExecutionID: id, Language: language, EnvironmentDigest: p.EnvironmentDigest, ProfileDigest: p.Digest, BudgetReservationRef: "cs1-synthetic", Budget: p.Limits, Network: network.NetworkScope{Mode: "none"}}
		payload, _ := json.Marshal(isolation.Payload{Code: code, Preload: ""})
		spec.InvocationDigest = isolation.InvocationDigest(spec, payload)
		body, _ := json.Marshal(isolation.InvocationRequest{Options: spec, Language: language, Code: code})
		resp, err := client.Post(endpoint+"/v1/sandbox/run", "application/json", bytes.NewReader(body))
		if err != nil {
			return isolation.ExecutionResult{}, err
		}
		defer resp.Body.Close()
		var result isolation.ExecutionResult
		if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return result, err
		}
		if resp.StatusCode != 200 {
			return result, fmt.Errorf("controller refused %s: HTTP %d (%s)", id, resp.StatusCode, result.FailureReason)
		}
		return result, nil
	}
	readObservation := func(id string) (isolation.Observation, error) {
		var o isolation.Observation
		raw, err := os.ReadFile(filepath.Join(observations, id+".json"))
		if err != nil {
			return o, fmt.Errorf("missing independent observation for %s: %w", id, err)
		}
		err = json.Unmarshal(raw, &o)
		if err != nil {
			return o, err
		}
		if o.Phase != "completed" || o.ExecutionID != id || !o.RootRemoved || !o.CgroupRemoved || o.Result.CleanupState != "clean" {
			return o, fmt.Errorf("unclean or incomplete observation for %s", id)
		}
		if len(o.Namespaces) != 5 || len(o.Controls) < 5 {
			return o, fmt.Errorf("namespace/cgroup setup did not complete for %s: %s", id, o.Result.FailureReason)
		}
		return o, nil
	}
	languages := []string{"python3", "nodejs"}
	codes := []string{"import os,time\nopen('/work/private','w').write('python')\nprint(os.getcwd(),flush=True)\ntime.sleep(0.25)", "const fs=require('fs');fs.writeFileSync('/work/private','node');console.log(process.cwd());setTimeout(()=>{},250)"}
	var wg sync.WaitGroup
	results := make([]isolation.ExecutionResult, 2)
	errs := make([]error, 2)
	for i := range languages {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = run("cs1-roots-"+strconv.Itoa(i), languages[i], codes[i])
		}(i)
	}
	wg.Wait()
	observationsSeen := make([]isolation.Observation, 2)
	for i := range results {
		if errs[i] != nil {
			return errs[i]
		}
		if results[i].Status != "succeeded" || strings.TrimSpace(results[i].Stdout) != "/work" {
			return fmt.Errorf("native %s cwd failed: %+v", languages[i], results[i])
		}
		o, err := readObservation(results[i].ExecutionID)
		if err != nil {
			return err
		}
		observationsSeen[i] = o
		for key, want := range map[string]string{"memory.max": strconv.FormatInt(p.Limits.MemoryBytes, 10), "memory.swap.max": "0", "memory.oom.group": "1", "pids.max": strconv.FormatInt(p.Limits.PIDs, 10), "cpu.max": fmt.Sprintf("%d %d", p.Limits.CPUQuotaMicros, p.Limits.CPUPeriodMicros)} {
			if o.Controls[key] != want {
				return fmt.Errorf("%s not enforced: %q", key, o.Controls[key])
			}
		}
	}
	if observationsSeen[0].Root == observationsSeen[1].Root || observationsSeen[0].Cgroup == observationsSeen[1].Cgroup {
		return fmt.Errorf("requests share writable root or cgroup")
	}
	for _, ns := range []string{"mnt", "pid", "ipc", "uts", "net"} {
		if observationsSeen[0].Namespaces[ns] == observationsSeen[1].Namespaces[ns] {
			return fmt.Errorf("requests share %s namespace", ns)
		}
	}
	// Every stress request runs alongside an ordinary peer. Natural kernel limit
	// failure or explicit supervisor failure is required; successful truncation is
	// never accepted. Each case is enumerated, so an empty discovery cannot pass.
	stresses := []struct{ name, code string }{
		{"memory", "x=bytearray(" + strconv.FormatInt(p.Limits.MemoryBytes*2, 10) + ")"},
		{"cpu", "while True: pass"},
		{"pid", "import os,time\nfor i in range(" + strconv.FormatInt(p.Limits.PIDs+8, 10) + "):\n if os.fork()==0: time.sleep(30);os._exit(0)\ntime.sleep(30)"},
		{"fd", "import sys\ntry:\n files=[open('/work/fd-'+str(i),'w') for i in range(" + strconv.FormatInt(p.Limits.FDs+8, 10) + ")]\nexcept OSError as e:\n print('FD_LIMIT:'+str(e.errno),file=sys.stderr);sys.exit(1)"},
		{"disk", "import sys\ntry:\n with open('/work/full','wb') as f:\n  for i in range(" + strconv.FormatInt(p.Limits.DiskBytes/4096+2, 10) + "): f.write(b'x'*4096)\nexcept OSError as e:\n print('DISK_LIMIT:'+str(e.errno),file=sys.stderr);sys.exit(1)"},
	}
	for _, stress := range stresses {
		var bad, peer isolation.ExecutionResult
		var badErr, peerErr error
		wg.Add(2)
		go func() { defer wg.Done(); bad, badErr = run("cs1-limit-"+stress.name, "python3", stress.code) }()
		go func() {
			defer wg.Done()
			peer, peerErr = run("cs1-peer-"+stress.name, "nodejs", "console.log('peer-alive')")
		}()
		wg.Wait()
		if badErr != nil {
			return badErr
		}
		if peerErr != nil {
			return peerErr
		}
		if bad.Status != "failed" || bad.CleanupState != "clean" {
			return fmt.Errorf("%s limit did not fail cleanly: %+v", stress.name, bad)
		}
		if peer.Status != "succeeded" || strings.TrimSpace(peer.Stdout) != "peer-alive" {
			return fmt.Errorf("peer died after %s exhaustion: %+v", stress.name, peer)
		}
		o, err := readObservation(bad.ExecutionID)
		if err != nil {
			return err
		}
		switch stress.name {
		case "cpu":
			if bad.FailureReason != "cpu_limit" {
				return fmt.Errorf("CPU exhaustion not independently confirmed: %s", bad.FailureReason)
			}
		case "memory":
			if !positiveEvent(o.Controls["memory.events"], "oom_kill") {
				return fmt.Errorf("memory cgroup exhaustion not observed")
			}
		case "pid":
			if !positiveEvent(o.Controls["pids.events"], "max") {
				return fmt.Errorf("PID cgroup exhaustion not observed (native seccomp may refuse fork earlier)")
			}
		case "fd":
			if !strings.Contains(bad.Stderr, "FD_LIMIT:24") {
				return fmt.Errorf("FD rlimit exhaustion not observed")
			}
		case "disk":
			if !strings.Contains(bad.Stderr, "DISK_LIMIT:28") && !strings.Contains(bad.Stderr, "DISK_LIMIT:27") {
				return fmt.Errorf("private filesystem exhaustion not observed")
			}
		}
		if _, err := readObservation(peer.ExecutionID); err != nil {
			return err
		}
	}
	// Serial reuse must not observe the prior request's file.
	serial, err := run("cs1-serial", "python3", "import os\nassert not os.path.exists('/work/private')\nprint('isolated')")
	if err != nil {
		return err
	}
	if serial.Status != "succeeded" || strings.TrimSpace(serial.Stdout) != "isolated" {
		return fmt.Errorf("serial request leaked writable state")
	}
	_, err = readObservation(serial.ExecutionID)
	return err
}

func positiveEvent(data, name string) bool {
	fields := strings.Fields(data)
	for i := 0; i+1 < len(fields); i += 2 {
		if fields[i] == name {
			n, err := strconv.ParseInt(fields[i+1], 10, 64)
			return err == nil && n > 0
		}
	}
	return false
}
