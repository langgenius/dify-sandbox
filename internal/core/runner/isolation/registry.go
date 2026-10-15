package isolation

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrInvocationConflict = errors.New("execution ID digest conflict")
var ErrExecutionUnknown = errors.New("execution state unavailable")

// Registry keeps bounded replay tombstones; it never evicts dispatch identity.
type Registry = registry

func (r *registry) Find(id string) (*Execution, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.entries[id]; ok {
		return e.execution, nil
	}
	return nil, ErrExecutionUnknown
}

type registryEntry struct {
	digest    string
	execution *Execution
}
type registry struct {
	mu      sync.Mutex
	limit   int64
	entries map[string]registryEntry
}

func newRegistry(limit int64) *registry {
	return &registry{limit: limit, entries: map[string]registryEntry{}}
}
func (r *registry) register(id, digest string, e *Execution) (*Execution, bool, error) {
	return r.claim(id, digest, func() (*Execution, error) { return e, nil })
}

// claim makes the first pending owner and its finite admission reservation one
// atomic operation. The callback is never invoked for a duplicate identity.
func (r *registry) claim(id, digest string, create func() (*Execution, error)) (*Execution, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.entries[id]; ok {
		if old.digest != digest {
			return nil, false, ErrInvocationConflict
		}
		return old.execution, false, nil
	}
	if int64(len(r.entries)) >= r.limit {
		return nil, false, failure("queue_full", "execution registry at capacity")
	}
	e, err := create()
	if err != nil {
		return nil, false, err
	}
	r.entries[id] = registryEntry{digest, e}
	return e, true, nil
}

// Completed entries remain as bounded replay tombstones. Capacity exhaustion
// refuses dispatch; eviction would allow a replay to execute code again.
type admission struct {
	mu                              sync.Mutex
	running, queued, bytes          int64
	quarantined                     int64
	maxRunning, maxQueued, maxBytes int64
	changed                         chan struct{}
}

func newAdmission(r, q, b int64) *admission {
	return &admission{maxRunning: r, maxQueued: q, maxBytes: b, changed: make(chan struct{})}
}
func (a *admission) notify()            { close(a.changed); a.changed = make(chan struct{}) }
func (a *admission) queuedCount() int64 { a.mu.Lock(); defer a.mu.Unlock(); return a.queued }

type admissionTicket struct {
	owner    *admission
	bytes    int64
	running  bool
	released bool
}

// reserve accounts for the queued payload synchronously, before the caller
// releases intake memory or starts any goroutine that retains that payload.
func (a *admission) reserve(bytes int64) (*admissionTicket, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.quarantined >= a.maxRunning {
		return nil, failure("unavailable", "all execution slots quarantined")
	}
	if bytes <= 0 || bytes > a.maxBytes-a.bytes {
		return nil, failure("queue_full", "service admission memory exhausted")
	}
	ticket := &admissionTicket{owner: a, bytes: bytes}
	if a.running < a.maxRunning && a.queued == 0 {
		ticket.running = true
		a.running++
	} else {
		if a.queued >= a.maxQueued {
			return nil, failure("queue_full", "service queue exhausted")
		}
		a.queued++
	}
	a.bytes += bytes
	a.notify()
	return ticket, nil
}
func (t *admissionTicket) release() {
	a := t.owner
	a.mu.Lock()
	defer a.mu.Unlock()
	t.releaseLocked()
}
func (t *admissionTicket) releaseLocked() {
	if t.released {
		return
	}
	t.released = true
	a := t.owner
	if t.running {
		a.running--
	} else {
		a.queued--
	}
	a.bytes -= t.bytes
	a.notify()
}
func (t *admissionTicket) wait(ctx context.Context, wait time.Duration) (func(), error) {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	a := t.owner
	for {
		a.mu.Lock()
		if ctx.Err() != nil {
			t.releaseLocked()
			a.mu.Unlock()
			return nil, ctx.Err()
		}
		if !t.running && a.running < a.maxRunning {
			a.queued--
			a.running++
			t.running = true
			a.notify()
		}
		if t.running {
			a.mu.Unlock()
			return t.release, nil
		}
		changed := a.changed
		a.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
		case <-timer.C:
			t.release()
			return nil, failure("queue_full", "queue deadline exceeded")
		}
	}
}
func (a *admission) acquire(ctx context.Context, bytes int64, wait time.Duration) (func(), error) {
	ticket, err := a.reserve(bytes)
	if err != nil {
		return nil, err
	}
	return ticket.wait(ctx, wait)
}

// Quarantine frees retained payload accounting, but never returns the slot.
func (t *admissionTicket) quarantine() {
	a := t.owner
	a.mu.Lock()
	defer a.mu.Unlock()
	if t.released {
		return
	}
	t.released = true
	if !t.running {
		a.queued--
		a.running++
	}
	a.quarantined++
	a.bytes -= t.bytes
	a.notify()
}
