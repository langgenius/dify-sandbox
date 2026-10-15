package isolation

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"time"
)

// ExecutionStatus deliberately excludes output, source, paths and environment.
// Result is returned separately only by Wait on an owned terminal execution.
type ExecutionStatus struct {
	ExecutionID      string `json:"execution_id"`
	InvocationDigest string `json:"invocation_digest"`
	State            string `json:"state"`
	Status           string `json:"status"`
	FailureReason    string `json:"failure_reason,omitempty"`
	CleanupState     string `json:"cleanup_state,omitempty"`
}

func (e *Execution) Snapshot() ExecutionStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := ExecutionStatus{ExecutionID: e.spec.ExecutionID, InvocationDigest: e.spec.InvocationDigest, State: e.state, Status: "unknown"}
	select {
	case <-e.done:
		s.Status = e.result.Status
		s.FailureReason = e.result.FailureReason
		s.CleanupState = e.result.CleanupState
	default:
		if s.State == "terminal" || s.State == "quarantined" {
			s.State = "terminating"
		}
	}
	return s
}
func (e *Execution) RequestTermination(reason string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state == "terminal" || e.state == "quarantined" || e.termination != "" {
		return
	}
	switch reason {
	case "stdout_limit", "stderr_limit", "cpu_limit", "deadline", "output_read", "invalid_output":
	default:
		reason = "cancelled"
	}
	e.termination = reason
	e.state = "terminating"
	if e.cancel != nil {
		e.cancel()
	}
}
func (e *Execution) Cancel(ctx context.Context, reason string) (ExecutionResult, error) {
	e.RequestTermination(reason)
	return e.Wait(ctx)
}
func (s *Supervisor) Lookup(id, digest string) (*Execution, error) {
	if !idPattern.MatchString(id) || !digestPattern.MatchString(digest) {
		return nil, ErrInvocationConflict
	}
	e, err := s.registry.Find(id)
	if err != nil {
		return nil, err
	}
	if e.spec.InvocationDigest != digest {
		return nil, ErrInvocationConflict
	}
	return e, nil
}

// Close releases the exclusive journal only once all owned executions finish.
func (s *Supervisor) Close() error {
	s.registry.mu.Lock()
	defer s.registry.mu.Unlock()
	for _, v := range s.registry.entries {
		select {
		case <-v.execution.done:
		default:
			return fmt.Errorf("executions still active")
		}
	}
	if s.closed {
		return nil
	}
	s.closed = true
	return s.journal.close()
}
func (e *Execution) terminateTree() error {
	// Called only after Start; the wait goroutine cannot reap/reuse this PID until
	// the monitor below is stopped. The cgroup remains the final descendant fence.
	if e.cmd == nil || e.cmd.Process == nil {
		return nil
	}
	err := unix.Kill(-e.cmd.Process.Pid, unix.SIGTERM)
	if err != nil && !errors.Is(err, unix.ESRCH) {
		return err
	}
	return nil
}
func (e *Execution) killTree() error {
	var groupErr error
	if e.cgroup != "" {
		if err := os.WriteFile(e.cgroup+"/cgroup.kill", []byte("1"), 0600); err != nil && !os.IsNotExist(err) {
			groupErr = err
		}
	}
	if e.cmd != nil && e.cmd.Process != nil {
		err := e.cmd.Process.Kill()
		if err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
	}
	return groupErr
}
func (e *Execution) terminationMonitor(ctx context.Context, done <-chan struct{}, stopped chan<- struct{}) {
	defer close(stopped)
	select {
	case <-done:
		return
	case <-ctx.Done():
	}
	reason := "cancelled"
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		reason = "deadline"
	}
	e.RequestTermination(reason)
	e.terminateTree()
	grace := time.NewTimer(time.Duration(e.spec.Budget.CleanupMillis) * time.Millisecond)
	defer grace.Stop()
	select {
	case <-done:
		return
	case <-grace.C:
		e.killTree()
	}
}
