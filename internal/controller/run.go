package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/langgenius/dify-sandbox/internal/service"
	"github.com/langgenius/dify-sandbox/internal/types"
)

func RunSandboxController(c *gin.Context) {
	BindRequest(c, func(req struct {
		Language string `json:"language" form:"language" binding:"required"`
		Code     string `json:"code" form:"code" binding:"required"`
	}) {
		switch req.Language {
		case "python3":
			c.JSON(200, service.RunPython3Code(req.Code))
		case "nodejs":
			c.JSON(200, service.RunNodeJsCode(req.Code))
		default:
			c.JSON(400, types.ErrorResponse(-400, "unsupported language"))
		}
	})
}
