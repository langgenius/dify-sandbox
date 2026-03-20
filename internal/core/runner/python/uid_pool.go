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

// ensurePasswdEntries writes sandbox UIDs to /etc/passwd in both the host
// and the chroot root so that Python's getpwuid on exit doesn't trigger
// syscalls blocked by seccomp.
func ensurePasswdEntries(min, max int) {
	chrootPasswd := "/var/sandbox/sandbox-python/etc/passwd"
	paths := []string{"/etc/passwd", chrootPasswd}

	for _, p := range paths {
		dir := p[:len(p)-len("passwd")]
		if err := os.MkdirAll(dir, 0755); err != nil {
			slog.Warn("failed to create dir for passwd", "path", p, "err", err)
			continue
		}
		f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			slog.Warn("failed to open passwd for UID entries", "path", p, "err", err)
			continue
		}
		for i := min; i < max; i++ {
			fmt.Fprintf(f, "sandbox%d:x:%d:0::/nonexistent:/usr/sbin/nologin\n", i, i)
		}
		f.Close()
		slog.Info("sandbox UID passwd entries created", "path", p, "range", fmt.Sprintf("%d-%d", min, max-1))
	}
}

func ReleaseUID(uid int) {
	globalPoolOnce.Do(func() {
		globalPool = NewUIDPool(10000, 11000)
	})
	globalPool.Release(uid)
}
