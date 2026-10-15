package isolation

import (
	"context"
	"time"
)

// A blocked filesystem operation cannot safely be abandoned with its capacity
// released. Only this owner continues toward launch; the verifier can only
// publish an error and close done. Late completion never resumes dispatch.
func (e *Execution) verifyBeforeLaunch(ctx context.Context, verify func() error, retain func()) bool {
	if err := ctx.Err(); err != nil {
		e.FailBeforeStart(err)
		return false
	}
	done := make(chan struct{})
	e.mu.Lock()
	e.verificationDone = done
	e.mu.Unlock()
	var err error
	go func() { defer close(done); err = verify() }()
	select {
	case <-done:
	case <-ctx.Done():
		timer := time.NewTimer(time.Duration(e.spec.Budget.CleanupMillis) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
			// Retain both slot and input reservation until supervisor replacement.
			// Close also keeps the journal lock while this goroutine is alive.
			e.quarantine = retain
			e.result.CleanupState = "quarantined"
			e.complete("unknown", "cleanup_failed")
			return false
		}
	}
	if cause := ctx.Err(); cause != nil {
		e.FailBeforeStart(cause)
		return false
	}
	if err != nil {
		e.complete("configuration_required", "environment_mismatch")
		return false
	}
	return true
}
