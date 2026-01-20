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

func MaxRequest(max int) gin.HandlerFunc {
	slog.Info("setting max requests", "max", max)
	m := &MaxRequestIface{
		current: 0,
		lock:    &sync.RWMutex{},
	}

	return func(c *gin.Context) {
		m.lock.RLock()
		if m.current >= max {
			m.lock.RUnlock()
			c.JSON(http.StatusServiceUnavailable, types.ErrorResponse(-503, "Too many requests"))
			c.Abort()
			return
		}
		m.lock.RUnlock()
		m.lock.Lock()
		m.current++
		m.lock.Unlock()
		c.Next()
		m.lock.Lock()
		m.current--
		m.lock.Unlock()
	}
}
