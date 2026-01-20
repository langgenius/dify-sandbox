package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/langgenius/dify-sandbox/internal/utils/log"
)

func TraceMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		traceparent := c.GetHeader("traceparent")
		traceID, spanID, ok := log.ParseTraceparent(traceparent)
		if !ok {
			traceID = log.GenerateTraceID()
			spanID = log.GenerateSpanID()
		}
		ctx = log.WithTrace(ctx, log.TraceContext{
			TraceID: traceID,
			SpanID:  spanID,
		})

		identity := log.Identity{
			TenantID: c.Param("tenant_id"),
			UserID:   c.GetHeader("X-User-ID"),
			UserType: c.GetHeader("X-User-Type"),
		}
		ctx = log.WithIdentity(ctx, identity)

		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
