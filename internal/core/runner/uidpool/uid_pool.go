package uidpool

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

var ErrUIDPoolExhausted = errors.New("sandbox UID pool exhausted")

type UIDPool struct {
	pool       chan int
	min        int
	max        int
	persistent *persistentPool
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
	for {
		select {
		case uid := <-p.pool:
			if p.persistent != nil {
				if err := p.persistent.acquire(uid); err != nil {
					slog.Warn("ordinary UID quarantined", "uid", uid, "err", err)
					continue
				}
			}
			return uid, nil
		case <-ctx.Done():
			return 0, ErrUIDPoolExhausted
		}
	}
}

func (p *UIDPool) Release(uid int) {
	if uid >= p.min && uid < p.max {
		if p.persistent != nil {
			if err := p.persistent.release(uid); err != nil {
				slog.Warn("ordinary UID remains quarantined", "uid", uid, "err", err)
				return
			}
		}
		p.pool <- uid
	}
}

func (p *UIDPool) Len() int {
	return len(p.pool)
}

var (
	globalPool     *UIDPool
	globalPoolOnce sync.Once
	globalPoolErr  error
)

func AcquireUID(ctx context.Context) (int, error) {
	globalPoolOnce.Do(func() {
		if err := os.MkdirAll(filepath.Dir(ordinaryLeaseRoot), 0755); err != nil {
			globalPoolErr = err
			return
		}
		globalPool, globalPoolErr = NewPersistentUIDPool(10000, 11000, ordinaryLeaseRoot)
		if globalPoolErr == nil {
			ensurePasswdEntries(10000, 11000)
		}
	})
	if globalPoolErr != nil {
		return 0, globalPoolErr
	}
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
	if globalPool != nil {
		globalPool.Release(uid)
	}
}

// VerifyUIDIdle independently observes Linux process UIDs before reuse.
// Durable ordinary leases retain resources and ownership across server restart.
func VerifyUIDIdle(uid int) error {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "status"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "Uid:") {
				for _, field := range strings.Fields(line)[1:] {
					v, err := strconv.Atoi(field)
					if err != nil {
						return err
					}
					if v == uid {
						return fmt.Errorf("UID %d still owns a process; quarantined", uid)
					}
				}
			}
		}
	}
	return nil
}

func ReleaseUIDChecked(uid int) error {
	if err := VerifyUIDIdle(uid); err != nil {
		return err
	}
	if globalPool == nil {
		return fmt.Errorf("UID pool is not initialized")
	}
	if globalPool.persistent != nil {
		if err := globalPool.persistent.release(uid); err != nil {
			return err
		}
		globalPool.pool <- uid
		return nil
	}
	ReleaseUID(uid)
	return nil
}
