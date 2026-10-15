package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/langgenius/dify-sandbox/internal/core/runner/isolation"
	"github.com/langgenius/dify-sandbox/internal/core/runner/network"
	"github.com/langgenius/dify-sandbox/tests/conformance/cases"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const orphanID = "cs1-lifecycle-orphan"

func probeInvocation(p isolation.Profile, id, code string) (isolation.InvocationSpec, []byte) {
	body, _ := json.Marshal(isolation.Payload{Code: code})
	s := isolation.InvocationSpec{Version: 1, ExecutionID: id, Language: "python3", EnvironmentDigest: p.EnvironmentDigest, ProfileDigest: p.Digest, BudgetReservationRef: "cs1-synthetic", Budget: p.Limits, Network: network.NetworkScope{Mode: "none"}}
	s.InvocationDigest = isolation.InvocationDigest(s, body)
	return s, body
}

const orphanCode = "import time\nprint('CS1_READY',flush=True)\nwhile True: time.sleep(1)"

func orphanProbe(p isolation.Profile, resolver isolation.ReservationResolver, factory isolation.LaunchFactory) error {
	var once sync.Once
	var mu sync.Mutex
	var cg, pidns string
	observer := func(o isolation.Observation) {
		mu.Lock()
		defer mu.Unlock()
		if o.Phase == "gated" {
			cg = o.Cgroup
			pidns = o.Namespaces["pid"]
		}
		if o.Phase == "stdout" && strings.Contains(o.StdoutChunk, "CS1_READY") {
			once.Do(func() {
				proof := orphanEvidence{PIDStarts: map[string]string{}, PIDNamespace: pidns}
				processes, err := os.ReadFile(filepath.Join(cg, "cgroup.procs"))
				if err != nil {
					panic(err)
				}
				for _, pid := range strings.Fields(string(processes)) {
					start, err := processStart(pid)
					if err != nil {
						panic(err)
					}
					proof.PIDStarts[pid] = start
					status, err := os.ReadFile("/proc/" + pid + "/status")
					if err != nil {
						panic(err)
					}
					for _, line := range strings.Split(string(status), "\n") {
						if strings.HasPrefix(line, "Uid:") {
							proof.UID = strings.Fields(line)[1]
						}
					}
				}
				if len(proof.PIDStarts) == 0 || proof.UID == "" || proof.UID == "0" {
					panic("native process/UID observation absent")
				}
				data, _ := json.Marshal(proof)
				if err := os.WriteFile("/observations/lifecycle-orphan-ready.json", data, 0600); err != nil {
					panic(err)
				}
			})
		}
	}
	s, err := isolation.NewSupervisor(p, resolver, factory, observer)
	if err != nil {
		return err
	}
	spec, body := probeInvocation(p, orphanID, orphanCode)
	e, err := s.Start(context.Background(), spec, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	r, err := e.Wait(context.Background())
	return fmt.Errorf("orphan must be killed while running, got %+v: %v", r, err)
}
func reconcileProbe(p isolation.Profile, resolver isolation.ReservationResolver, factory isolation.LaunchFactory) error {
	if p.MaxConcurrent < 2 {
		return fmt.Errorf("quarantine probe requires two slots")
	}
	warmRead, warmWrite, err := os.Pipe()
	if err != nil {
		return err
	}
	warmRead.Close()
	warmWrite.Close()
	before, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return err
	}
	proof := cases.LifecycleReconcileEvidence{FDsBefore: len(before), ProfileDigest: p.Digest, EnvironmentDigest: p.EnvironmentDigest}
	ready := "/observations/lifecycle-orphan-ready.json"
	os.Remove(ready)
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	child := exec.Command(executable, "--lifecycle-orphan")
	child.Stdout = os.Stderr
	child.Stderr = os.Stderr
	if err = child.Start(); err != nil {
		return err
	}
	until := time.Now().Add(time.Duration(p.Limits.WallMillis) * time.Millisecond)
	found := false
	for time.Now().Before(until) {
		if _, err = os.Stat(ready); err == nil {
			found = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	child.Process.Kill()
	child.Wait()
	if !found {
		return fmt.Errorf("actual orphan never reached filtered native readiness")
	}
	orphanData, err := os.ReadFile(ready)
	if err != nil {
		return err
	}
	var orphan orphanEvidence
	if err = json.Unmarshal(orphanData, &orphan); err != nil {
		return err
	}
	calls := 0
	counted := func(s isolation.InvocationSpec, p isolation.Payload) (isolation.Launch, error) {
		calls++
		return factory(s, p)
	}
	s, err := isolation.NewSupervisor(p, resolver, counted)
	if err != nil {
		return err
	}
	spec, body := probeInvocation(p, orphanID, orphanCode)
	e, err := s.Start(context.Background(), spec, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	r, err := e.Wait(context.Background())
	if err != nil {
		return err
	}
	proof.RestartUnknown = r.Status == "unknown" && r.FailureReason == "result_unavailable" && r.CleanupState == "clean"
	proof.NoReplay = calls == 0
	for pid, start := range orphan.PIDStarts {
		current, err := processStart(pid)
		if err == nil && current == start {
			proof.SurvivingProcesses++
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if !proof.RestartUnknown || !proof.NoReplay || proof.SurvivingProcesses != 0 {
		return fmt.Errorf("restart failed: %+v", r)
	}
	if err = s.Close(); err != nil {
		return err
	}
	var activeRoot string
	var rootMu sync.Mutex
	readySignal := make(chan struct{}, 1)
	observer := func(o isolation.Observation) {
		if o.Phase == "gated" && o.ExecutionID == "cs1-lifecycle-quarantine" {
			rootMu.Lock()
			activeRoot = o.Root
			rootMu.Unlock()
		}
		if o.Phase == "stdout" && strings.Contains(o.StdoutChunk, "CS1_READY") {
			select {
			case readySignal <- struct{}{}:
			default:
			}
		}
	}
	s, err = isolation.NewSupervisor(p, resolver, factory, observer)
	if err != nil {
		return err
	}
	spec, body = probeInvocation(p, "cs1-lifecycle-quarantine", orphanCode)
	e, err = s.Start(context.Background(), spec, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	select {
	case <-readySignal:
	case <-time.After(time.Duration(p.Limits.WallMillis) * time.Millisecond):
		return fmt.Errorf("quarantine workload not ready")
	}
	// Service-side fault injection only: no new child permission or public route.
	rootMu.Lock()
	root := activeRoot
	rootMu.Unlock()
	held := root + "-held"
	if err = os.Rename(root, held); err != nil {
		return err
	}
	if err = os.Symlink(held, root); err != nil {
		return err
	}
	r, err = e.Cancel(context.Background(), "cancelled")
	if err != nil {
		return err
	}
	proof.Quarantine = r.CleanupState == "quarantined" && r.FailureReason == "cleanup_failed"
	peerSpec, peerBody := probeInvocation(p, "cs1-lifecycle-quarantine-peer", "import os\nprint(os.getuid())")
	peer, err := s.Start(context.Background(), peerSpec, strings.NewReader(string(peerBody)))
	if err != nil {
		return err
	}
	peerResult, err := peer.Wait(context.Background())
	if err != nil {
		return err
	}
	proof.PeerSucceeded = peerResult.Status == "succeeded" && peerResult.CleanupState == "clean"
	proof.UIDReuseVerified = strings.TrimSpace(peerResult.Stdout) == orphan.UID
	if err = s.Close(); err != nil {
		return err
	}
	if err = os.Remove(root); err != nil {
		return err
	}
	if err = os.Rename(held, root); err != nil {
		return err
	}
	s, err = isolation.NewSupervisor(p, resolver, factory)
	if err != nil {
		return err
	}
	old, err := s.Lookup(spec.ExecutionID, spec.InvocationDigest)
	if err != nil {
		return err
	}
	state := old.Snapshot()
	_, rootErr := os.Stat(root)
	proof.Reconciled = state.CleanupState == "clean" && state.Status == "unknown" && os.IsNotExist(rootErr)
	if err = s.Close(); err != nil {
		return err
	}
	after, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return err
	}
	proof.FDsAfter = len(after)
	if !proof.UIDReuseVerified || !proof.Quarantine || !proof.PeerSucceeded || !proof.Reconciled || proof.FDsAfter > proof.FDsBefore {
		return fmt.Errorf("actual lifecycle probe failed: %+v", proof)
	}
	data, _ := json.Marshal(proof)
	target := filepath.Join("/observations", "lifecycle-reconcile.json")
	return os.WriteFile(target, data, 0644)
}

type orphanEvidence struct {
	PIDStarts         map[string]string
	UID, PIDNamespace string
}

func processStart(pid string) (string, error) {
	data, err := os.ReadFile("/proc/" + pid + "/stat")
	if err != nil {
		return "", err
	}
	end := strings.LastIndex(string(data), ") ")
	if end < 0 {
		return "", fmt.Errorf("invalid process identity")
	}
	fields := strings.Fields(string(data)[end+2:])
	if len(fields) < 20 {
		return "", fmt.Errorf("short process identity")
	}
	return fields[19], nil
}
