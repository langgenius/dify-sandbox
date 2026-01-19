package service

import (
	"context"
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner/nodejs"
	"github.com/langgenius/dify-sandbox/internal/core/runner/nodejs_microsandbox"
	runner_types "github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/static"
	"github.com/langgenius/dify-sandbox/internal/types"
)

func RunNodeJsCode(
	ctx context.Context,
	code string,
	preload string,
	options *runner_types.RunnerOptions) *types.DifySandboxResponse {
	if err := checkOptions(options); err != nil {
		return types.ErrorResponse(-400, err.Error())
	}

	if !static.GetDifySandboxGlobalConfigurations().EnablePreload {
		preload = ""
	}

	timeout := time.Duration(
		static.GetDifySandboxGlobalConfigurations().WorkerTimeout * int(time.Second),
	)

	// Select runner based on configuration
	var runner runner_types.CodeRunner

	if !static.GetDifySandboxGlobalConfigurations().Sandbox.Enabled {
		// Default to native runner
		runner = &nodejs.NodeJsRunner{}
	} else {
		runner = &nodejs_microsandbox.NodeJSMicroSandboxRunner{}
	}

	stdout, stderr, done, err := runner.Run(ctx, code, timeout, nil, preload, options)
	if err != nil {
		return types.ErrorResponse(-500, err.Error())
	}

	stdout_str := ""
	stderr_str := ""

	for {
		select {
		case out := <-stdout:
			stdout_str += string(out)
		case err := <-stderr:
			stderr_str += string(err)
		case <-done:
			// Drain any remaining buffered output before returning
		drain:
			for {
				select {
				case out := <-stdout:
					stdout_str += string(out)
				case err := <-stderr:
					stderr_str += string(err)
				default:
					break drain
				}
			}
			return types.SuccessResponse(&RunCodeResponse{
				Stdout: stdout_str,
				Stderr: stderr_str,
			})
		}
	}
}
