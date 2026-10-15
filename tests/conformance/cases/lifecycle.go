package cases

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/langgenius/dify-sandbox/internal/core/runner/isolation"
	"github.com/langgenius/dify-sandbox/internal/core/runner/network"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Lifecycle requires real native execution and private cleanup observations.
// The separately executed candidate restart probe supplies evidence that cannot
// be manufactured through the public HTTP API.
func Lifecycle(endpoint, profilePath, observations string) error {
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
	client := &http.Client{Timeout: time.Duration(p.Limits.WallMillis+p.Limits.QueueMillis+3*p.Limits.CleanupMillis+2000) * time.Millisecond}
	prefix := fmt.Sprintf("cs1-lifecycle-%d-", time.Now().UnixNano())
	invoke := func(id, lang, code string, b isolation.Budget) (isolation.InvocationRequest, []byte) {
		spec := isolation.InvocationSpec{Version: 1, ExecutionID: prefix + id, Language: lang, EnvironmentDigest: p.EnvironmentDigest, ProfileDigest: p.Digest, BudgetReservationRef: "cs1-synthetic", Budget: b, Network: network.NetworkScope{Mode: "none"}}
		payload, _ := json.Marshal(isolation.Payload{Code: code})
		spec.InvocationDigest = isolation.InvocationDigest(spec, payload)
		r := isolation.InvocationRequest{Options: spec, Language: lang, Code: code}
		body, _ := json.Marshal(r)
		return r, body
	}
	send := func(ctx context.Context, body []byte) (isolation.ExecutionResult, error) {
		req, _ := http.NewRequestWithContext(ctx, "POST", endpoint+"/v1/sandbox/run", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Api-Key", os.Getenv("SANDBOX_API_KEY"))
		resp, err := client.Do(req)
		if err != nil {
			return isolation.ExecutionResult{}, err
		}
		defer resp.Body.Close()
		var r isolation.ExecutionResult
		err = json.NewDecoder(io.LimitReader(resp.Body, 6*(p.Limits.StdoutBytes+p.Limits.StderrBytes)+8192)).Decode(&r)
		if resp.StatusCode != 200 {
			return r, fmt.Errorf("run HTTP %d: %s", resp.StatusCode, r.FailureReason)
		}
		return r, err
	}
	evidence := func(id string) error {
		o, err := readEvidence(observations, id)
		if err != nil {
			return err
		}
		if o.Phase != "completed" || len(o.Namespaces) != 5 || !o.RootRemoved || !o.CgroupRemoved || o.Result.CleanupState != "clean" {
			return fmt.Errorf("actual cleanup evidence absent for %s", id)
		}
		return nil
	}
	workloads := []struct{ name, lang, code, reason string }{
		{"stderr", "python3", "import sys\nwhile True: sys.stderr.write('x'*4096);sys.stderr.flush()", "stderr_limit"},
		{"stdout", "nodejs", "while(true)console.log('x'.repeat(4096))", "stdout_limit"},
		{"utf8", "python3", "import sys\nsys.stdout.buffer.write('€x'.encode());sys.stdout.flush()", "stdout_limit"},
		{"pipes", "python3", "import os,time\npid=os.fork()\nif pid==0:\n while True: time.sleep(1)\nelse:\n print('SPAWNED',flush=True)\n time.sleep(0.05)\n os._exit(0)", ""},
	}
	for _, w := range workloads {
		b := p.Limits
		if w.name == "utf8" {
			b.StdoutBytes = 3
		}
		r, body := invoke(w.name, w.lang, w.code, b)
		result, err := send(context.Background(), body)
		if err != nil {
			return err
		}
		wantStatus := "failed"
		if w.reason == "" {
			wantStatus = "succeeded"
		}
		if result.Status != wantStatus || result.FailureReason != w.reason || result.CleanupState != "clean" {
			return fmt.Errorf("%s did not exercise expected native limit: %+v", w.name, result)
		}
		if int64(len(result.Stdout)) > b.StdoutBytes || int64(len(result.Stderr)) > b.StderrBytes {
			return fmt.Errorf("retained output exceeded bytes")
		}
		if err = evidence(r.Options.ExecutionID); err != nil {
			return err
		}
	}
	lifecycle := func(r isolation.InvocationRequest, method, digest string) (int, isolation.ExecutionStatus, error) {
		path := endpoint + "/v1/sandbox/executions/" + r.Options.ExecutionID
		if method == "POST" {
			path += "/cancel"
		}
		req, _ := http.NewRequest(method, path, nil)
		req.Header.Set("X-Api-Key", os.Getenv("SANDBOX_API_KEY"))
		req.Header.Set("X-Invocation-Digest", digest)
		resp, err := client.Do(req)
		if err != nil {
			return 0, isolation.ExecutionStatus{}, err
		}
		defer resp.Body.Close()
		var s isolation.ExecutionStatus
		err = json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&s)
		return resp.StatusCode, s, err
	}
	// Occupy every real slot, then disconnect a confirmed queued invocation.
	// This proves a pre-dispatch disconnect cannot cross the start gate.
	if p.MaxConcurrent > 16 {
		return fmt.Errorf("lifecycle harness requires at most 16 synthetic slots")
	}
	var stopHolders []context.CancelFunc
	var holderResponses []chan error
	defer func() {
		for _, stop := range stopHolders {
			stop()
		}
	}()
	for i := int64(0); i < p.MaxConcurrent; i++ {
		holder, body := invoke(fmt.Sprintf("queue-holder-%d", i), "python3", "import time\nprint('CS1_READY',flush=True)\nwhile True: time.sleep(1)", p.Limits)
		ctx, stop := context.WithCancel(context.Background())
		stopHolders = append(stopHolders, stop)
		response := make(chan error, 1)
		holderResponses = append(holderResponses, response)
		go func() { _, err := send(ctx, body); response <- err }()
		until := time.Now().Add(time.Duration(p.Limits.WallMillis) * time.Millisecond)
		ready := false
		for time.Now().Before(until) {
			o, err := readEvidence(observations, holder.Options.ExecutionID)
			if err == nil && o.ReadyNanos > 0 {
				ready = true
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if !ready {
			return fmt.Errorf("queue holder did not reach native readiness")
		}
	}
	queued, queuedBody := invoke("disconnect-before-dispatch", "python3", "print('MUST_NOT_RUN')", p.Limits)
	queuedCtx, disconnectQueued := context.WithCancel(context.Background())
	queuedResponse := make(chan error, 1)
	go func() { _, err := send(queuedCtx, queuedBody); queuedResponse <- err }()
	untilQueued := time.Now().Add(time.Duration(p.Limits.QueueMillis) * time.Millisecond)
	observedQueue := false
	for time.Now().Before(untilQueued) {
		_, state, err := lifecycle(queued, "GET", queued.Options.InvocationDigest)
		if err == nil && state.State == "queued" {
			observedQueue = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	disconnectQueued()
	<-queuedResponse
	if !observedQueue {
		return fmt.Errorf("pre-dispatch disconnect did not exercise queue")
	}
	terminalQueue := false
	untilQueued = time.Now().Add(time.Duration(p.Limits.CleanupMillis+1000) * time.Millisecond)
	for time.Now().Before(untilQueued) {
		_, state, err := lifecycle(queued, "GET", queued.Options.InvocationDigest)
		if err == nil && state.State == "terminal" && state.Status == "cancelled" {
			terminalQueue = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !terminalQueue {
		return fmt.Errorf("queued disconnect was not cancelled")
	}
	observed, err := readEvidence(observations, queued.Options.ExecutionID)
	if err != nil || observed.StartedNanos != 0 || observed.ReadyNanos != 0 {
		return fmt.Errorf("queued disconnect reached native execution")
	}
	for _, stop := range stopHolders {
		stop()
	}
	for _, response := range holderResponses {
		<-response
	}
	for _, disconnect := range []bool{false, true} {
		name := "cancel"
		if disconnect {
			name = "disconnect"
		}
		r, body := invoke(name, "python3", "import signal,time\nsignal.signal(signal.SIGTERM,signal.SIG_IGN)\nprint('CS1_READY',flush=True)\nwhile True: time.sleep(1)", p.Limits)
		ctx, cancel := context.WithCancel(context.Background())
		response := make(chan error, 1)
		go func() { _, err := send(ctx, body); response <- err }()
		until := time.Now().Add(time.Duration(p.Limits.WallMillis) * time.Millisecond)
		ready := false
		for time.Now().Before(until) {
			o, err := readEvidence(observations, r.Options.ExecutionID)
			if err == nil && o.ReadyNanos > 0 {
				ready = true
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if !ready {
			cancel()
			return fmt.Errorf("native TERM-ignoring workload never became ready")
		}
		code, _, err := lifecycle(r, "POST", strings.Repeat("f", 64))
		if err != nil || code != 409 {
			cancel()
			return fmt.Errorf("wrong digest accepted")
		}
		if disconnect {
			cancel()
		} else {
			for i := 0; i < 2; i++ {
				code, _, err = lifecycle(r, "POST", r.Options.InvocationDigest)
				if err != nil || code != 200 {
					cancel()
					return fmt.Errorf("duplicate cancellation failed")
				}
			}
		}
		<-response
		cancel()
		until = time.Now().Add(time.Duration(3*p.Limits.CleanupMillis+1000) * time.Millisecond)
		terminal := false
		for time.Now().Before(until) {
			_, s, err := lifecycle(r, "GET", r.Options.InvocationDigest)
			if err != nil {
				return err
			}
			if s.State == "terminal" {
				if s.Status != "cancelled" || s.CleanupState != "clean" {
					return fmt.Errorf("wrong cancellation: %+v", s)
				}
				terminal = true
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if !terminal {
			return fmt.Errorf("cancellation did not terminate within cleanup bound")
		}
		result, err := send(context.Background(), body)
		if err != nil || result.Status != "cancelled" {
			return fmt.Errorf("same ID replayed after lost response")
		}
		if err = evidence(r.Options.ExecutionID); err != nil {
			return err
		}
	}
	r, body := invoke("healthy-peer", "nodejs", "console.log('healthy')", p.Limits)
	result, err := send(context.Background(), body)
	if err != nil {
		return err
	}
	if result.Status != "succeeded" {
		return fmt.Errorf("subsequent healthy peer failed: %+v", result)
	}
	if err = evidence(r.Options.ExecutionID); err != nil {
		return err
	}
	probe, err := os.ReadFile(filepath.Join(observations, "lifecycle-reconcile.json"))
	if err != nil {
		return fmt.Errorf("actual candidate restart/quarantine probe required: %w", err)
	}
	var proof LifecycleReconcileEvidence
	if err = json.Unmarshal(probe, &proof); err != nil {
		return err
	}
	if proof.ProfileDigest != p.Digest || proof.EnvironmentDigest != p.EnvironmentDigest || proof.SurvivingProcesses != 0 || !proof.UIDReuseVerified || !proof.RestartUnknown || !proof.NoReplay || !proof.Quarantine || !proof.PeerSucceeded || !proof.Reconciled || proof.FDsAfter > proof.FDsBefore {
		return fmt.Errorf("incomplete lifecycle reconciliation proof: %+v", proof)
	}
	return nil
}

type LifecycleReconcileEvidence struct {
	ProfileDigest      string `json:"profile_digest"`
	EnvironmentDigest  string `json:"environment_digest"`
	SurvivingProcesses int    `json:"surviving_processes"`
	UIDReuseVerified   bool   `json:"uid_reuse_verified"`
	RestartUnknown     bool   `json:"restart_unknown"`
	NoReplay           bool   `json:"no_replay"`
	Quarantine         bool   `json:"quarantine"`
	PeerSucceeded      bool   `json:"peer_succeeded"`
	Reconciled         bool   `json:"reconciled"`
	FDsBefore          int    `json:"fds_before"`
	FDsAfter           int    `json:"fds_after"`
}
