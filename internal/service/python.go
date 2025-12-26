package service

import (
	"context"
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner/python"
	"github.com/langgenius/dify-sandbox/internal/core/runner/python_microsandbox"
	runner_types "github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/static"
	"github.com/langgenius/dify-sandbox/internal/types"
)

type RunCodeResponse struct {
	Stderr string `json:"error"`
	Stdout string `json:"stdout"`
}

func RunPython3Code(
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
	backend := static.GetDifySandboxGlobalConfigurations().SandboxBackend

	if backend == "microsandbox" && static.GetDifySandboxGlobalConfigurations().Sandbox.Enabled {
		runner = &python_microsandbox.PythonMicroSandboxRunner{}
	} else {
		// Default to native runner
		runner = &python.PythonRunner{}
	}

	stdout, stderr, done, err := runner.Run(ctx,
		code, timeout, nil, preload, options,
	)
	if err != nil {
		return types.ErrorResponse(-500, err.Error())
	}

	stdoutStr := ""
	stderrStr := ""

	// Read from stdout/stderr until we receive done, then drain any remaining buffered data
	for {
		select {
		case out := <-stdout:
			stdoutStr += string(out)
		case err := <-stderr:
			stderrStr += string(err)
		case <-done:
			// Drain any remaining buffered output to avoid races where done is selected first
		drain:
			for {
				select {
				case out := <-stdout:
					stdoutStr += string(out)
				case err := <-stderr:
					stderrStr += string(err)
				default:
					break drain
				}
			}
			return types.SuccessResponse(&RunCodeResponse{
				Stdout: stdoutStr,
				Stderr: stderrStr,
			})
		}
	}
}

type ListDependenciesResponse struct {
	Dependencies []runner_types.Dependency `json:"dependencies"`
}

func ListPython3Dependencies() *types.DifySandboxResponse {
	var deps []runner_types.Dependency

	if !static.GetDifySandboxGlobalConfigurations().Sandbox.Enabled {
		deps = python.ListDependencies()
	}

	return types.SuccessResponse(&ListDependenciesResponse{
		Dependencies: deps,
	})
}

type RefreshDependenciesResponse struct {
	Dependencies []runner_types.Dependency `json:"dependencies"`
}

func RefreshPython3Dependencies() *types.DifySandboxResponse {
	var deps []runner_types.Dependency

	if !static.GetDifySandboxGlobalConfigurations().Sandbox.Enabled {
		deps = python.RefreshDependencies()
	}

	return types.SuccessResponse(&RefreshDependenciesResponse{
		Dependencies: deps,
	})
}

type UpdateDependenciesResponse struct{}

func UpdateDependencies() *types.DifySandboxResponse {
	var err error

	if !static.GetDifySandboxGlobalConfigurations().Sandbox.Enabled {
		err = python.PreparePythonDependenciesEnv()
	}

	if err != nil {
		return types.ErrorResponse(-500, err.Error())
	}

	return types.SuccessResponse(&UpdateDependenciesResponse{})
}
