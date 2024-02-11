package middleware

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/langgenius/dify-sandbox/internal/types"
	"github.com/langgenius/dify-sandbox/internal/utils/log"
)

func MaxWorker(max int) gin.HandlerFunc {
	log.Info("setting max workers to %d", max)
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
	log.Info("setting max requests to %d", max)
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
