package isolation

import (
	"context"
	"fmt"
	"sync"
	"time"
)

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
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.entries[id]; ok {
		if old.digest != digest {
			return nil, false, fmt.Errorf("execution ID digest conflict")
		}
		return old.execution, false, nil
	}
	if int64(len(r.entries)) >= r.limit {
		return nil, false, failure("queue_full", "execution registry at capacity")
	}
	r.entries[id] = registryEntry{digest, e}
	return e, true, nil
}

// Completed entries remain as bounded replay tombstones. Capacity exhaustion
// refuses dispatch; eviction would allow a replay to execute code again.
type admission struct {
	mu                              sync.Mutex
	running, queued, bytes          int64
	maxRunning, maxQueued, maxBytes int64
	changed                         chan struct{}
}

func newAdmission(r, q, b int64) *admission {
	return &admission{maxRunning: r, maxQueued: q, maxBytes: b, changed: make(chan struct{})}
}
func (a *admission) notify()            { close(a.changed); a.changed = make(chan struct{}) }
func (a *admission) queuedCount() int64 { a.mu.Lock(); defer a.mu.Unlock(); return a.queued }
func (a *admission) acquire(ctx context.Context, bytes int64, wait time.Duration) (func(), error) {
	a.mu.Lock()
	if bytes <= 0 || bytes > a.maxBytes-a.bytes || a.queued >= a.maxQueued {
		a.mu.Unlock()
		return nil, failure("queue_full", "service admission capacity exhausted")
	}
	a.bytes += bytes
	a.queued++
	a.notify()
	a.mu.Unlock()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		a.mu.Lock()
		if ctx.Err() != nil {
			a.queued--
			a.bytes -= bytes
			a.notify()
			a.mu.Unlock()
			return nil, ctx.Err()
		}
		if a.running < a.maxRunning {
			a.queued--
			a.running++
			a.notify()
			a.mu.Unlock()
			var once sync.Once
			return func() {
				once.Do(func() { a.mu.Lock(); defer a.mu.Unlock(); a.running--; a.bytes -= bytes; a.notify() })
			}, nil
		}
		changed := a.changed
		a.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
		case <-timer.C:
			a.mu.Lock()
			a.queued--
			a.bytes -= bytes
			a.notify()
			a.mu.Unlock()
			return nil, failure("queue_full", "queue deadline exceeded")
		}
	}
}
func (r *registry) lookup(id, digest string) (*Execution, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.entries[id]; ok {
		if old.digest != digest {
			return nil, fmt.Errorf("execution ID digest conflict")
		}
		return old.execution, nil
	}
	return nil, nil
}
