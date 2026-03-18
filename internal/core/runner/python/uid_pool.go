package python

import (
	"errors"
	"sync"
)

const (
	uidPoolMin = 10000
	uidPoolMax = 11000
)

var (
	uidPool     chan int
	uidPoolOnce sync.Once
)

func initUIDPool() {
	uidPool = make(chan int, uidPoolMax-uidPoolMin)
	for i := uidPoolMin; i < uidPoolMax; i++ {
		uidPool <- i
	}
}

// AcquireUID takes an unused UID from the pool.
// Returns an error when all UIDs are currently in use.
func AcquireUID() (int, error) {
	uidPoolOnce.Do(initUIDPool)
	select {
	case uid := <-uidPool:
		return uid, nil
	default:
		return 0, errors.New("sandbox UID pool exhausted")
	}
}

// ReleaseUID returns a UID back to the pool for reuse.
func ReleaseUID(uid int) {
	if uid >= uidPoolMin && uid < uidPoolMax {
		uidPool <- uid
	}
}
