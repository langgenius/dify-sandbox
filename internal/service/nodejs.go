package service

import (
	"context"
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner/nodejs"
	runner_types "github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/static"
	"github.com/langgenius/dify-sandbox/internal/types"
)

func RunNodeJsCode(ctx context.Context, code string, preload string, options *runner_types.RunnerOptions) *types.DifySandboxResponse {
	if err := checkOptions(options); err != nil {
		return types.ErrorResponse(-400, err.Error())
	}

	if !static.GetDifySandboxGlobalConfigurations().EnablePreload {
		preload = ""
	}

	timeout := time.Duration(
		static.GetDifySandboxGlobalConfigurations().WorkerTimeout * int(time.Second),
	)

	runner := nodejs.NodeJsRunner{}
	result, err := runner.Run(ctx, code, timeout, nil, preload, options)
	if err != nil {
		return types.ErrorResponse(-500, err.Error())
	}

	return types.SuccessResponse(collectRunCodeResponse(result))
}
