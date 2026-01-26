package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/langgenius/dify-sandbox/internal/static"
	appLog "github.com/langgenius/dify-sandbox/internal/utils/log"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Identity middleware extracts tenant/user identity from path/header, attaches to context,
// and sets tenant.id attribute on the current span when OpenTelemetry is enabled.
func Identity() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		// Prefer path param :tenant_id, fallback to headers
		tenantID := c.Param("tenant_id")
		if tenantID == "" {
			tenantID = c.GetHeader("X-Tenant-ID")
			if tenantID == "" {
				tenantID = c.GetHeader("X-Tenant-Id")
			}
		}

		userID := c.GetHeader("X-User-ID")
		userType := c.GetHeader("X-User-Type")

		if tenantID != "" || userID != "" || userType != "" {
			ctx = appLog.WithIdentity(ctx, appLog.Identity{TenantID: tenantID, UserID: userID, UserType: userType})
		}

		// If tracing enabled, annotate the active HTTP span.
		if static.GetDifySandboxGlobalConfigurations().Otel.Enable && tenantID != "" {
			span := trace.SpanFromContext(ctx)
			sc := span.SpanContext()
			if sc.IsValid() {
				span.SetAttributes(attribute.String("tenant.id", tenantID))
			}
		}

		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}