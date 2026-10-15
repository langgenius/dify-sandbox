package controller

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/langgenius/dify-sandbox/internal/core/runner/isolation"
	runner_types "github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/service"
	"github.com/langgenius/dify-sandbox/internal/types"
)

func RunSandboxController(c *gin.Context) {
	BindRequest(c, func(req struct {
		Language      string `json:"language" form:"language" binding:"required"`
		Code          string `json:"code" form:"code" binding:"required"`
		Preload       string `json:"preload" form:"preload"`
		EnableNetwork bool   `json:"enable_network" form:"enable_network"`
	}) {
		switch req.Language {
		case "python3":
			c.JSON(200, service.RunPython3Code(c, req.Code, req.Preload, &runner_types.RunnerOptions{
				EnableNetwork: req.EnableNetwork,
			}))
		case "nodejs":
			c.JSON(200, service.RunNodeJsCode(c, req.Code, req.Preload, &runner_types.RunnerOptions{
				EnableNetwork: req.EnableNetwork,
			}))
		default:
			c.JSON(400, types.ErrorResponse(-400, "unsupported language"))
		}
	})
}

func GetDependencies(c *gin.Context) {
	BindRequest(c, func(req struct {
		Language string `json:"language" form:"language" binding:"required"`
	}) {
		switch req.Language {
		case "python3":
			c.JSON(200, service.ListPython3Dependencies())
		default:
			c.JSON(400, types.ErrorResponse(-400, "unsupported language"))
		}
	})
}

func UpdateDependencies(c *gin.Context) {
	BindRequest(c, func(req struct {
		Language string `json:"language" form:"language" binding:"required"`
	}) {
		switch req.Language {
		case "python3":
			c.JSON(200, service.UpdateDependencies())
		default:
			c.JSON(400, types.ErrorResponse(-400, "unsupported language"))
		}
	})
}

func RefreshDependencies(c *gin.Context) {
	BindRequest(c, func(req struct {
		Language string `json:"language" form:"language" binding:"required"`
	}) {
		switch req.Language {
		case "python3":
			c.JSON(200, service.RefreshPython3Dependencies())
		default:
			c.JSON(400, types.ErrorResponse(-400, "unsupported language"))
		}
	})
}

// NewBuilderRunController is bound by server composition. It never falls back
// to ordinary execution when options are absent or malformed.
func NewBuilderRunController(supervisor *isolation.Supervisor) gin.HandlerFunc {
	return func(c *gin.Context) {
		if supervisor == nil {
			c.JSON(503, isolation.ExecutionResult{Status: "configuration_required", FailureReason: "environment_mismatch", CleanupState: "clean", ExitCode: -1})
			return
		}
		execution, err := supervisor.StartRequest(c.Request.Context(), c.Request.Body)
		if err != nil {
			status := 400
			reason := "input_limit"
			var failure *isolation.Failure
			if errors.As(err, &failure) {
				reason = failure.Reason
				if reason == "queue_full" {
					status = 429
				}
			}
			c.JSON(status, isolation.ExecutionResult{Status: "failed", FailureReason: reason, CleanupState: "clean", ExitCode: -1})
			return
		}
		result, err := execution.Wait(c.Request.Context())
		if err != nil {
			return
		}
		c.JSON(200, result)
	}
}
