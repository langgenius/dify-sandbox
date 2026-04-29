package service

import (
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner/nodejs"
	"github.com/langgenius/dify-sandbox/internal/pool"
	runner_types "github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/static"
	"github.com/langgenius/dify-sandbox/internal/types"
)

func RunNodeJsCode(code string, preload string, options *runner_types.RunnerOptions) *types.DifySandboxResponse {
	if err := checkOptions(options); err != nil {
		return types.ErrorResponse(-400, err.Error())
	}

	if !static.GetDifySandboxGlobalConfigurations().EnablePreload {
		preload = ""
	}

	timeout := time.Duration(
		static.GetDifySandboxGlobalConfigurations().WorkerTimeout * int(time.Second),
	)

	// --- pool mode ---
	if globalPool != nil {
		task := &pool.PoolTask{
			Type:    pool.TaskTypeNodeJS,
			Code:    code,
			Preload: preload,
			Timeout: timeout,
			Options: &pool.RunnerOptions{EnableNetwork: options.EnableNetwork},
		}
		result, err := globalPool.Submit(task)
		if err != nil {
			return types.ErrorResponse(-500, err.Error())
		}
		return drainPoolResult(result)
	}

	// --- original fork mode ---
	runner := nodejs.NodeJsRunner{}
	stdout, stderr, done, err := runner.Run(code, timeout, nil, preload, options)
	if err != nil {
		return types.ErrorResponse(-500, err.Error())
	}
	return drainChannels(stdout, stderr, done)
}
