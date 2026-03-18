package python

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestAcquireAndRelease(t *testing.T) {
	pool := NewUIDPool(10000, 10005)
	ctx := context.Background()

	uid, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uid < 10000 || uid >= 10005 {
		t.Fatalf("uid %d out of range [10000, 10005)", uid)
	}

	pool.Release(uid)

	uid2, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("unexpected error after release: %v", err)
	}
	if uid2 < 10000 || uid2 >= 10005 {
		t.Fatalf("uid %d out of range after release", uid2)
	}
}

func TestPoolExhaustion(t *testing.T) {
	pool := NewUIDPool(10000, 10005)
	ctx := context.Background()

	acquired := make([]int, 0, 5)
	for i := 0; i < 5; i++ {
		uid, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatalf("failed to acquire uid #%d: %v", i, err)
		}
		acquired = append(acquired, uid)
	}

	// Pool exhausted: acquire with short timeout should fail
	timeoutCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	_, err := pool.Acquire(timeoutCtx)
	if err != ErrUIDPoolExhausted {
		t.Fatalf("expected ErrUIDPoolExhausted, got %v", err)
	}

	// Release one, should be able to acquire again
	pool.Release(acquired[0])
	uid, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("expected acquire to succeed after release: %v", err)
	}
	if uid != acquired[0] {
		t.Fatalf("expected uid %d back, got %d", acquired[0], uid)
	}
}

func TestPoolWaitsForRelease(t *testing.T) {
	pool := NewUIDPool(10000, 10001) // only 1 UID
	ctx := context.Background()

	uid, _ := pool.Acquire(ctx)

	// Launch a goroutine that releases after 100ms
	go func() {
		time.Sleep(100 * time.Millisecond)
		pool.Release(uid)
	}()

	// Acquire should block and succeed once the UID is released
	start := time.Now()
	timeoutCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	uid2, err := pool.Acquire(timeoutCtx)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected acquire to succeed after waiting, got: %v", err)
	}
	if uid2 != uid {
		t.Fatalf("expected uid %d, got %d", uid, uid2)
	}
	if elapsed < 80*time.Millisecond {
		t.Fatalf("acquire returned too fast (%v), should have waited for release", elapsed)
	}
}

func TestUIDUniqueness(t *testing.T) {
	pool := NewUIDPool(10000, 10005)
	ctx := context.Background()

	seen := make(map[int]bool)
	for i := 0; i < 5; i++ {
		uid, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatalf("failed to acquire uid #%d: %v", i, err)
		}
		if seen[uid] {
			t.Fatalf("duplicate uid %d at acquisition #%d", uid, i)
		}
		seen[uid] = true
	}
	if len(seen) != 5 {
		t.Fatalf("expected 5 unique UIDs, got %d", len(seen))
	}
}

func TestConcurrentAcquireRelease(t *testing.T) {
	pool := NewUIDPool(10000, 10005)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			timeoutCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			uid, err := pool.Acquire(timeoutCtx)
			if err != nil {
				return
			}
			_ = uid * uid
			pool.Release(uid)
		}()
	}
	wg.Wait()

	if pool.Len() != 5 {
		t.Fatalf("pool size after concurrent test: got %d, want 5", pool.Len())
	}
}

func TestReleaseInvalidUID(t *testing.T) {
	pool := NewUIDPool(10000, 10005)

	before := pool.Len()
	pool.Release(99999)
	pool.Release(-1)
	pool.Release(9999)
	after := pool.Len()

	if after != before {
		t.Fatalf("invalid release changed pool size: before=%d, after=%d", before, after)
	}
}

func TestReleaseRestoresCapacity(t *testing.T) {
	pool := NewUIDPool(10000, 10005)
	ctx := context.Background()

	acquired := make([]int, 0, 5)
	for i := 0; i < 5; i++ {
		uid, _ := pool.Acquire(ctx)
		acquired = append(acquired, uid)
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if _, err := pool.Acquire(timeoutCtx); err != ErrUIDPoolExhausted {
		t.Fatal("pool should be exhausted")
	}

	for _, uid := range acquired {
		pool.Release(uid)
	}

	if pool.Len() != 5 {
		t.Fatalf("pool size after release all: got %d, want 5", pool.Len())
	}

	seen := make(map[int]bool)
	for i := 0; i < 5; i++ {
		uid, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatalf("acquire #%d failed after restore: %v", i, err)
		}
		if uid < 10000 || uid >= 10005 {
			t.Fatalf("uid %d out of range", uid)
		}
		if seen[uid] {
			t.Fatalf("duplicate uid %d", uid)
		}
		seen[uid] = true
	}
}
