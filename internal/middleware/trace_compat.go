package middleware

import "github.com/gin-gonic/gin"

// TraceMiddleware is kept for backward compatibility with existing router wiring.
// It currently delegates to Identity(), which records tenant information and sets
// tenant.id attribute on the current span when OpenTelemetry is enabled.
func TraceMiddleware() gin.HandlerFunc {
	return Identity()
}