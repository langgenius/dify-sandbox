package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/langgenius/dify-sandbox/internal/middleware"
)

func Setup(eng *gin.Engine) {
	eng.Use(middleware.Auth())

	eng.POST("/v1/sandbox/run", RunSandboxController)
}
