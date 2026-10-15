package cases

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/langgenius/dify-sandbox/internal/core/runner/isolation"
	"github.com/langgenius/dify-sandbox/internal/core/runner/network"
	"net/http"
	"os"
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
	if p.MaxConcurrent < 2 || p.Limits.Concurrent < 2 {
		return fmt.Errorf("roots-and-budgets requires at least two concurrent execution slots")
	}
	if p.Limits.WallMillis < 4 {
		return fmt.Errorf("wall budget too small for finite peer workload")
	}
	client := &http.Client{Timeout: time.Duration(p.Limits.WallMillis+p.Limits.QueueMillis+p.Limits.CleanupMillis+1000) * time.Millisecond}
	run := func(id, language, code string, budget isolation.Budget) (isolation.ExecutionResult, error) {
		spec := isolation.InvocationSpec{Version: 1, ExecutionID: id, Language: language, EnvironmentDigest: p.EnvironmentDigest, ProfileDigest: p.Digest, BudgetReservationRef: "cs1-synthetic", Budget: budget, Network: network.NetworkScope{Mode: "none"}}
		payload, _ := json.Marshal(isolation.Payload{Code: code, Preload: ""})
		spec.InvocationDigest = isolation.InvocationDigest(spec, payload)
		body, _ := json.Marshal(isolation.InvocationRequest{Options: spec, Language: language, Code: code})
		req, _ := http.NewRequest("POST", endpoint+"/v1/sandbox/run", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Api-Key", os.Getenv("SANDBOX_API_KEY"))
		resp, err := client.Do(req)
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
	readObservation := func(id string) (Evidence, error) {
		o, err := readEvidence(observations, id)
		if err != nil {
			return o, fmt.Errorf("missing independent observation for %s: %w", id, err)
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
	codes := []string{"import os,time\nopen('/work/private','w').write('python')\nprint('CS1_READY',flush=True)\nprint(os.getcwd(),flush=True)\ntime.sleep(0.25)", "const fs=require('fs');fs.writeFileSync('/work/private','node');console.log('CS1_READY');console.log(process.cwd());setTimeout(()=>{},250)"}
	var wg sync.WaitGroup
	results := make([]isolation.ExecutionResult, 2)
	errs := make([]error, 2)
	for i := range languages {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = run("cs1-roots-"+strconv.Itoa(i), languages[i], codes[i], p.Limits)
		}(i)
	}
	wg.Wait()
	observationsSeen := make([]Evidence, 2)
	for i := range results {
		if errs[i] != nil {
			return errs[i]
		}
		if results[i].Status != "succeeded" || strings.TrimSpace(workloadOutput(results[i].Stdout)) != "/work" {
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
	if err := requireOverlap(observationsSeen[0], observationsSeen[1]); err != nil {
		return err
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
		peerID := "cs1-peer-" + stress.name
		type response struct {
			result isolation.ExecutionResult
			err    error
		}
		peerDone := make(chan response, 1)
		peerCode := fmt.Sprintf("console.log('CS1_READY');const h=setInterval(()=>console.log('CS1_ALIVE'),25);setTimeout(()=>{clearInterval(h);console.log('peer-alive')},%d)", p.Limits.WallMillis/2)
		go func() { result, err := run(peerID, "nodejs", peerCode, p.Limits); peerDone <- response{result, err} }()
		readyCtx, stop := context.WithTimeout(context.Background(), time.Duration(p.Limits.WallMillis)*time.Millisecond)
		readyErr := waitReady(readyCtx, observations, peerID)
		stop()
		if readyErr != nil {
			<-peerDone
			return readyErr
		}
		offenderBudget := p.Limits
		offenderBudget.WallMillis = p.Limits.WallMillis / 4
		bad, badErr := run("cs1-limit-"+stress.name, "python3", "print('CS1_READY',flush=True)\n"+stress.code, offenderBudget)
		peerResponse := <-peerDone
		peer, peerErr := peerResponse.result, peerResponse.err
		if badErr != nil {
			return badErr
		}
		if peerErr != nil {
			return peerErr
		}
		if bad.Status != "failed" || bad.CleanupState != "clean" {
			return fmt.Errorf("%s limit did not fail cleanly: %+v", stress.name, bad)
		}
		if peer.Status != "succeeded" || strings.TrimSpace(workloadOutput(peer.Stdout)) != "peer-alive" {
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
		peerEvidence, err := readObservation(peer.ExecutionID)
		if err != nil {
			return err
		}
		if err = requirePeerSpan(o, peerEvidence); err != nil {
			return err
		}
	}
	// Serial reuse must not observe the prior request's file.
	serial, err := run("cs1-serial", "python3", "import os\nassert not os.path.exists('/work/private')\nprint('isolated')", p.Limits)
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

func waitReady(ctx context.Context, directory, id string) error {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		e, err := readEvidence(directory, id)
		if err == nil {
			if e.Phase == "completed" {
				return fmt.Errorf("peer finished before offender admission: %s", id)
			}
			if e.ReadyNanos > 0 {
				return nil
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("peer readiness not observed: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
