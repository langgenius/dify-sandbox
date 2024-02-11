package middleware

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/langgenius/dify-sandbox/internal/types"
	"github.com/langgenius/dify-sandbox/internal/utils/log"
)

func MaxWoker(max int) gin.HandlerFunc {
	queue := make(chan *gin.Context, max)

	for i := 0; i < max; i++ {
		i := i
		go func() {
			log.Info("code runner worker %d started", i)
			for {
				select {
				case c := <-queue:
					c.Next()
				}
			}
		}()
	}

	return func(c *gin.Context) {
		queue <- c
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
