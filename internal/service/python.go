package service

import (
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner/python"
	runner_types "github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/static"
	"github.com/langgenius/dify-sandbox/internal/types"
)

type RunCodeResponse struct {
	Stderr string            `json:"error"`
	Stdout string            `json:"stdout"`
	Files  map[string][]byte `json:"files"`
}

func RunPython3Code(code string, preload string, options *runner_types.RunnerOptions) *types.DifySandboxResponse {
	if err := checkOptions(options); err != nil {
		return types.ErrorResponse(-400, err.Error())
	}

	if !static.GetDifySandboxGlobalConfigurations().EnablePreload {
	    preload = ""
	}

	timeout := time.Duration(
		static.GetDifySandboxGlobalConfigurations().WorkerTimeout * int(time.Second),
	)

	// ... inside RunPython3Code ...
	runner := python.PythonRunner{}
	stdout, stderr, done, filesChan, err := runner.Run(
		code, timeout, nil, preload, options,
	)
	if err != nil {
		return types.ErrorResponse(-500, err.Error())
	}

	stdout_str := ""
	stderr_str := ""
    var files map[string][]byte

	defer close(done)
	defer close(stdout)
	defer close(stderr)
    // filesChan is closed by runner

	for {
		select {
		case <-done:
			// Attempt to read files if available and not yet read
			if files == nil && filesChan != nil {
				select {
				case f, ok := <-filesChan:
					if ok {
						files = f
					}
				default:
				}
			}
			return types.SuccessResponse(&RunCodeResponse{
				Stdout: stdout_str,
				Stderr: stderr_str,
				Files:  files,
			})
		case out := <-stdout:
			stdout_str += string(out)
		case err := <-stderr:
			stderr_str += string(err)
		case f, ok := <-filesChan:
			if ok {
				files = f
			}
			filesChan = nil // Stop listening to avoid busy loop on closed channel
		}
	}
}

type ListDependenciesResponse struct {
	Dependencies []runner_types.Dependency `json:"dependencies"`
}

func ListPython3Dependencies() *types.DifySandboxResponse {
	return types.SuccessResponse(&ListDependenciesResponse{
		Dependencies: python.ListDependencies(),
	})
}

type RefreshDependenciesResponse struct {
	Dependencies []runner_types.Dependency `json:"dependencies"`
}

func RefreshPython3Dependencies() *types.DifySandboxResponse {
	return types.SuccessResponse(&RefreshDependenciesResponse{
		Dependencies: python.RefreshDependencies(),
	})
}

type UpdateDependenciesResponse struct{}

func UpdateDependencies() *types.DifySandboxResponse {
	err := python.PreparePythonDependenciesEnv()
	if err != nil {
		return types.ErrorResponse(-500, err.Error())
	}

	return types.SuccessResponse(&UpdateDependenciesResponse{})
}
