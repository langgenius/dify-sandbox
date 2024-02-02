package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/langgenius/dify-sandbox/internal/service"
	"github.com/langgenius/dify-sandbox/internal/static"
	"github.com/langgenius/dify-sandbox/internal/types"
)

var (
	queue chan bool
)

func InitSandBoxQueue() {
	if queue == nil {
		queue = make(chan bool, static.GetCoshubGlobalConfigurations().MaxWorkers)
	}
}

func RunSandboxController(c *gin.Context) {

	BindRequest(c, func(req struct {
		Language string `json:"language" form:"language" binding:"required"`
		Code     string `json:"code" form:"code" binding:"required"`
	}) {
		queue <- true
		defer func() {
			<-queue
		}()
		switch req.Language {
		case "python3":
			c.JSON(200, service.RunPython3Code(req.Code))
		default:
			c.JSON(400, types.ErrorResponse(-400, "unsupported language"))
		}
	})
}
