package service

import (
	"context"
	"strings"
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
	stdout, stderr, done, err := runner.Run(ctx, code, timeout, nil, preload, options)
	if err != nil {
		return types.ErrorResponse(-500, err.Error())
	}

	var stdoutStr strings.Builder
	var stderrStr strings.Builder

	defer close(done)

	for {
		select {
		case <-done:
			// Drain any remaining buffered output to avoid races
		drain:
			for {
				select {
				case out := <-stdout:
					stdoutStr.Write(out)
				case err := <-stderr:
					stderrStr.Write(err)
				default:
					break drain
				}
			}
			// Close channels after draining all data
			close(stdout)
			close(stderr)
			return types.SuccessResponse(&RunCodeResponse{
				Stdout: stdoutStr.String(),
				Stderr: stderrStr.String(),
			})
		case out := <-stdout:
			stdoutStr.Write(out)
		case err := <-stderr:
			stderrStr.Write(err)
		}
	}
}
