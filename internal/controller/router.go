package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/langgenius/dify-sandbox/internal/core/runner/isolation"
	"github.com/langgenius/dify-sandbox/internal/middleware"
	"github.com/langgenius/dify-sandbox/internal/static"
)

func Setup(Router *gin.Engine) {
	PublicGroup := Router.Group("")
	PrivateGroup := Router.Group("/v1/sandbox/")

	PrivateGroup.Use(middleware.Auth())

	{
		// health check
		PublicGroup.GET("/health", func(c *gin.Context) {
			c.JSON(http.StatusOK, "ok")
		})
	}

	InitRunRouter(PrivateGroup)
	InitDependencyRouter(PrivateGroup)
}

func InitDependencyRouter(Router *gin.RouterGroup) {
	dependencyRouter := Router.Group("dependencies")
	{
		dependencyRouter.GET("", GetDependencies)
		dependencyRouter.POST("update", UpdateDependencies)
		dependencyRouter.GET("refresh", RefreshDependencies)
	}
}

func InitRunRouter(Router *gin.RouterGroup) {
	runRouter := Router.Group("")
	{
		runRouter.POST(
			"run",
			middleware.MaxRequest(static.GetDifySandboxGlobalConfigurations().MaxRequests),
			middleware.MaxWorker(static.GetDifySandboxGlobalConfigurations().MaxWorkers),
			middleware.TraceMiddleware(),
			RunSandboxController,
		)
	}
}

// SetupBuilder installs an explicitly separate deployment profile at the same
// wire endpoint. Ordinary Setup and its dependency-refresh behavior are intact.
// The owning HTTP server must configure ReadHeaderTimeout and ReadTimeout.
func SetupBuilder(router *gin.Engine, supervisor *isolation.Supervisor) {
	private := router.Group("/v1/sandbox/")
	private.Use(func(c *gin.Context) {
		if static.GetDifySandboxGlobalConfigurations().App.Key == "" {
			c.AbortWithStatus(http.StatusServiceUnavailable)
		}
	})
	private.Use(middleware.Auth())
	private.POST("run", NewBuilderRunController(supervisor))
	private.GET("executions/:id", NewExecutionController(supervisor, false))
	private.POST("executions/:id/cancel", NewExecutionController(supervisor, true))
	router.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, "ok") })
}
