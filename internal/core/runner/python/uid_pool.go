package python

import (
	"context"
	"errors"
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
	})
	return globalPool.Acquire(ctx)
}

func ReleaseUID(uid int) {
	globalPoolOnce.Do(func() {
		globalPool = NewUIDPool(10000, 11000)
	})
	globalPool.Release(uid)
}
