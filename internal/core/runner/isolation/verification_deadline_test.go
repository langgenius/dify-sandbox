package isolation

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Delayed and uninterruptible operations exercise the real admission owner,
// cancellation, journal and launch boundary without relying on a slow filesystem.
func TestVerificationDeadlineAndCancellation(t *testing.T) {
	for _, mode := range []string{"cancel", "deadline", "blocked-cancel", "blocked-deadline"} {
		t.Run(mode, func(t *testing.T) {
			p := testProfile(t)
			p.Limits.WallMillis = 120
			p.Limits.CleanupMillis = 40
			var launches atomic.Int32
			s, err := NewSupervisor(p, func(context.Context, InvocationSpec) (Budget, error) { return p.Limits, nil }, func(InvocationSpec, Payload) (Launch, error) {
				launches.Add(1)
				return Launch{}, errors.New("must not launch")
			})
			if err != nil {
				t.Fatal(err)
			}
			entered, unblock, exited := make(chan struct{}), make(chan struct{}), make(chan struct{})
			blocked := strings.HasPrefix(mode, "blocked")
			s.verify = func(ctx context.Context, _, _ string) error {
				close(entered)
				defer close(exited)
				if blocked {
					<-unblock
					return nil
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-unblock:
					return nil
				}
			}
			var unblockOnce sync.Once
			stop := func() { unblockOnce.Do(func() { close(unblock) }) }
			defer func() { stop(); <-exited; s.Close() }()
			spec := testSpec(p)
			payload := `{"code":"print(1)","preload":""}`
			spec.InvocationDigest = InvocationDigest(spec, []byte(payload))
			e, err := s.Start(context.Background(), spec, strings.NewReader(payload))
			if err != nil {
				t.Fatal(err)
			}
			<-entered
			if strings.HasSuffix(mode, "cancel") {
				time.Sleep(20 * time.Millisecond)
				e.RequestTermination("cancelled")
			}
			wait, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
			defer cancel()
			r, err := e.Wait(wait)
			if err != nil {
				t.Fatalf("verification failed to settle within wall + cleanup allowance: %v", err)
			}
			if launches.Load() != 0 {
				t.Fatal("cancelled verification launched")
			}
			if e.WorkDir != "" || e.cmd != nil || e.ownedPID != 0 {
				t.Fatal("verification cancellation reached worker creation")
			}
			if r.Usage.WallMillis < 15 {
				t.Fatalf("verification time unaccounted: %+v", r)
			}
			if blocked {
				if r.Status != "unknown" || r.CleanupState != "quarantined" {
					t.Fatalf("blocked verifier not quarantined: %+v", r)
				}
				if err := s.Close(); err == nil {
					t.Fatal("journal unlocked while verification still active")
				}
				if replacement, err := NewSupervisor(p, s.reserve, s.launch); err == nil {
					replacement.Close()
					t.Fatal("second supervisor acquired live verifier's journal")
				}
				s.admission.mu.Lock()
				running, quarantined, retained := s.admission.running, s.admission.quarantined, s.admission.bytes
				s.admission.mu.Unlock()
				if running != 1 || quarantined != 1 || retained != spec.Budget.InputBytes {
					t.Fatalf("lost blocked operation accounting: %d %d %d", running, quarantined, retained)
				}
			} else if strings.HasSuffix(mode, "cancel") {
				if r.Status != "cancelled" || r.CleanupState != "clean" {
					t.Fatalf("cancel result: %+v", r)
				}
			} else if r.Status != "failed" || r.FailureReason != "deadline" || r.Usage.WallMillis < 100 {
				t.Fatalf("deadline result: %+v", r)
			}
			replayed, err := s.Start(context.Background(), spec, strings.NewReader(payload))
			if err != nil || replayed != e {
				t.Fatalf("lost replay owner: %v", err)
			}
			if blocked {
				next := spec
				next.ExecutionID = "request-2"
				next.InvocationDigest = InvocationDigest(next, []byte(payload))
				if _, err := s.Start(context.Background(), next, strings.NewReader(payload)); err == nil {
					t.Fatal("quarantined capacity reused")
				}
				stop()
				<-exited
				<-e.verificationDone
				if launches.Load() != 0 {
					t.Fatal("late verification launched code")
				}
				s.admission.mu.Lock()
				retained := s.admission.bytes
				s.admission.mu.Unlock()
				if retained != spec.Budget.InputBytes {
					t.Fatal("late verification silently returned capacity")
				}
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
				restarted, err := NewSupervisor(p, func(context.Context, InvocationSpec) (Budget, error) { return p.Limits, nil }, s.launch)
				if err != nil {
					t.Fatal(err)
				}
				defer restarted.Close()
				old, err := restarted.Start(context.Background(), spec, strings.NewReader(payload))
				if err != nil {
					t.Fatal(err)
				}
				result, err := old.Wait(wait)
				if err != nil || result.Status != "unknown" || result.FailureReason != "result_unavailable" || launches.Load() != 0 {
					t.Fatalf("restart replayed uncertain execution: %+v %v", result, err)
				}
			}
		})
	}
}

func TestWallDeadlineIncludesAdmissionQueue(t *testing.T) {
	p := testProfile(t)
	p.Limits.WallMillis = 50
	p.Limits.QueueMillis = 500
	var launches atomic.Int32
	s, err := NewSupervisor(p, func(context.Context, InvocationSpec) (Budget, error) { return p.Limits, nil }, func(InvocationSpec, Payload) (Launch, error) { launches.Add(1); return Launch{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ticket, err := s.admission.reserve(1)
	if err != nil {
		t.Fatal(err)
	}
	defer ticket.release()
	spec := testSpec(p)
	payload := `{"code":"print(1)","preload":""}`
	spec.InvocationDigest = InvocationDigest(spec, []byte(payload))
	e, err := s.Start(context.Background(), spec, strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	r, err := e.Wait(ctx)
	if err != nil || r.FailureReason != "deadline" || r.Usage.WallMillis < 40 || launches.Load() != 0 {
		t.Fatalf("queue escaped wall budget: %+v %v", r, err)
	}
}
