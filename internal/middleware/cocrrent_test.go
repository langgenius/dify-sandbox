package middleware

import (
	"sync"
	"testing"
)

func TestMaxRequestTryAcquireRespectsLimitUnderConcurrency(t *testing.T) {
	m := &MaxRequestIface{
		current: 0,
		lock:    &sync.RWMutex{},
	}

	const (
		max        = 1
		goroutines = 32
	)

	start := make(chan struct{})
	results := make(chan bool, goroutines)

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- m.tryAcquire(max)
		}()
	}

	close(start)
	wg.Wait()
	close(results)

	successes := 0
	failures := 0
	for acquired := range results {
		if acquired {
			successes++
			continue
		}
		failures++
	}

	if successes != max {
		t.Fatalf("expected %d successful acquisition, got %d", max, successes)
	}

	if failures != goroutines-max {
		t.Fatalf("expected %d failed acquisitions, got %d", goroutines-max, failures)
	}

	if m.current != max {
		t.Fatalf("expected current to remain %d, got %d", max, m.current)
	}
}

func TestMaxRequestReleaseRestoresCapacity(t *testing.T) {
	m := &MaxRequestIface{
		current: 0,
		lock:    &sync.RWMutex{},
	}

	if !m.tryAcquire(1) {
		t.Fatal("expected first acquisition to succeed")
	}

	if m.tryAcquire(1) {
		t.Fatal("expected second acquisition to fail while limit is reached")
	}

	m.release()

	if !m.tryAcquire(1) {
		t.Fatal("expected acquisition to succeed after release")
	}
}
