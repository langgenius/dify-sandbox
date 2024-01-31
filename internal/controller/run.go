package controller

import "github.com/gin-gonic/gin"

func RunSandboxController(c *gin.Context) {
	BindRequest(c, func(req struct {
		Language string `json:"language" form:"language" binding:"required"`
		Version  string `json:"version" form:"version" binding:"required"`
		Code     string `json:"code" form:"code" binding:"required"`
	}) {

	})
}
