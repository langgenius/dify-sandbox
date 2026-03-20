package python

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
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

// Global singleton
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

// ensurePasswdEntries appends sandbox UIDs to /etc/passwd so that
// Python's cleanup (e.g. getpwuid) doesn't trigger blocked syscalls.
func ensurePasswdEntries(min, max int) {
	f, err := os.OpenFile("/etc/passwd", os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		slog.Warn("failed to open /etc/passwd for UID entries", "err", err)
		return
	}
	defer f.Close()
	for i := min; i < max; i++ {
		fmt.Fprintf(f, "sandbox%d:x:%d:0::/nonexistent:/usr/sbin/nologin\n", i, i)
	}
	slog.Info("sandbox UID passwd entries created", "range", fmt.Sprintf("%d-%d", min, max-1))
}

func ReleaseUID(uid int) {
	globalPoolOnce.Do(func() {
		globalPool = NewUIDPool(10000, 11000)
	})
	globalPool.Release(uid)
}
