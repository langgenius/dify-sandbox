package middleware

import (
	"log/slog"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/langgenius/dify-sandbox/internal/types"
)

func MaxWorker(max int) gin.HandlerFunc {
	slog.Info("setting max workers", "max", max)
	sem := make(chan struct{}, max)

	return func(c *gin.Context) {
		sem <- struct{}{}
		defer func() {
			<-sem
		}()
		c.Next()
	}
}

type MaxRequestIface struct {
	current int
	lock    *sync.RWMutex
}

func (m *MaxRequestIface) tryAcquire(max int) bool {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.current >= max {
		return false
	}

	m.current++
	return true
}

func (m *MaxRequestIface) release() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.current--
}

func MaxRequest(max int) gin.HandlerFunc {
	slog.Info("setting max requests", "max", max)
	m := &MaxRequestIface{
		current: 0,
		lock:    &sync.RWMutex{},
	}

	return func(c *gin.Context) {
		if !m.tryAcquire(max) {
			c.JSON(http.StatusServiceUnavailable, types.ErrorResponse(-503, "Too many requests"))
			c.Abort()
			return
		}
		defer m.release()

		c.Next()
	}
}
