package middleware

import (
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"
)

// TraceIDHeader sets the current trace id on the response header after the handler runs.
// Requires otel instrumentation (e.g., otelgin) to run earlier in the chain to ensure a span exists.
func TraceIDHeader() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		span := trace.SpanFromContext(c.Request.Context())
		sc := span.SpanContext()
		if sc.IsValid() {
			c.Writer.Header().Set("Trace-Id", sc.TraceID().String())
		}
	}
}
