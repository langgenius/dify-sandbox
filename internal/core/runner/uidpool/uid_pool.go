package uidpool

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
)

var ErrUIDPoolExhausted = errors.New("sandbox UID pool exhausted")

type UIDPool struct {
	pool chan int
	min  int
	max  int
}

func NewUIDPool(min, max int) *UIDPool {
	p := &UIDPool{
		pool: make(chan int, max-min),
		min:  min,
		max:  max,
	}
	for i := min; i < max; i++ {
		p.pool <- i
	}
	return p
}

// Acquire waits for an available UID until ctx is cancelled.
func (p *UIDPool) Acquire(ctx context.Context) (int, error) {
	select {
	case uid := <-p.pool:
		return uid, nil
	case <-ctx.Done():
		return 0, ErrUIDPoolExhausted
	}
}

func (p *UIDPool) Release(uid int) {
	if uid >= p.min && uid < p.max {
		p.pool <- uid
	}
}

func (p *UIDPool) Len() int {
	return len(p.pool)
}

var (
	globalPool     *UIDPool
	globalPoolOnce sync.Once
)

func AcquireUID(ctx context.Context) (int, error) {
	globalPoolOnce.Do(func() {
		globalPool = NewUIDPool(10000, 11000)
		ensurePasswdEntries(10000, 11000)
	})
	return globalPool.Acquire(ctx)
}

// ensurePasswdEntries validates that sandbox UID entries exist in /etc/passwd
// Performs exact matching on the complete entry format
func ensurePasswdEntries(min, max int) {
	// Open /etc/passwd in read-only mode
	f, err := os.Open("/etc/passwd")
	if err != nil {
		slog.Error("failed to open /etc/passwd", "err", err)
		return
	}
	defer f.Close()

	// Read and parse file line by line
	scanner := bufio.NewScanner(f)

	// Build a set of expected exact strings
	expected := make(map[string]bool)
	for i := min; i < max; i++ {
		expected[fmt.Sprintf("sandbox%d:x:%d:0::/nonexistent:/usr/sbin/nologin", i, i)] = true
	}

	// Track found entries
	found := make(map[string]bool)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if expected[line] {
			found[line] = true
		}
	}

	if err := scanner.Err(); err != nil {
		slog.Error("error reading /etc/passwd", "err", err)
		return
	}

	// Find missing entries
	var missingUIDs []int
	for i := min; i < max; i++ {
		expectedLine := fmt.Sprintf("sandbox%d:x:%d:0::/nonexistent:/usr/sbin/nologin", i, i)
		if !found[expectedLine] {
			missingUIDs = append(missingUIDs, i)
		}
	}

	// Report errors
	if len(missingUIDs) > 0 {
		slog.Error("sandbox UID entries missing or incorrect in /etc/passwd",
			"missing_uids", missingUIDs,
			"count", len(missingUIDs),
			"expected_format", "sandbox${UID}:x:${UID}:0::/nonexistent:/usr/sbin/nologin",
			"example", fmt.Sprintf("sandbox%d:x:%d:0::/nonexistent:/usr/sbin/nologin", min, min))
	} else {
		slog.Info("sandbox UID entries verified",
			"range", fmt.Sprintf("%d-%d", min, max-1),
			"count", max-min)
	}
}

func ReleaseUID(uid int) {
	globalPoolOnce.Do(func() {
		globalPool = NewUIDPool(10000, 11000)
	})
	globalPool.Release(uid)
}
