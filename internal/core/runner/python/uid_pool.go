package python

import (
	"context"

	"github.com/langgenius/dify-sandbox/internal/core/runner/uidpool"
)

var ErrUIDPoolExhausted = uidpool.ErrUIDPoolExhausted

type UIDPool = uidpool.UIDPool

func NewUIDPool(min, max int) *UIDPool {
	return uidpool.NewUIDPool(min, max)
}

func AcquireUID(ctx context.Context) (int, error) {
	return uidpool.AcquireUID(ctx)
}

func ReleaseUID(uid int) {
	uidpool.ReleaseUID(uid)
}
