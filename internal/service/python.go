package service

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner/python"
	runner_types "github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/static"
	"github.com/langgenius/dify-sandbox/internal/storage"
	"github.com/langgenius/dify-sandbox/internal/types"
)

type RunCodeResponse struct {
	Stderr string            `json:"error"`
	Stdout string            `json:"stdout"`
	Files  map[string]string `json:"files"`
}

// inputFiles is map[filename]file_id
func RunPython3Code(code string, preload string, enableNetwork bool, inputFiles map[string]string, fetchFiles []string) *types.DifySandboxResponse {
	// Reconstruct options
	// Note: We are creating RunnerOptions here now, instead of receiving it
	options := &runner_types.RunnerOptions{
		EnableNetwork: enableNetwork,
		FetchFiles:    fetchFiles,
		InputFiles:    make(map[string]io.Reader),
	}

	if err := checkOptions(options); err != nil {
		return types.ErrorResponse(-400, err.Error())
	}

	// Prepare Input Files
	store := storage.GetStorage()
	var readersToClose []io.ReadCloser
	defer func() {
		for _, r := range readersToClose {
			r.Close()
		}
	}()

	for filename, fileId := range inputFiles {
		reader, err := store.Get(fileId)
		if err != nil {
			return types.ErrorResponse(-400, fmt.Sprintf("failed to get input file %s: %v", filename, err))
		}
		options.InputFiles[filename] = reader
		readersToClose = append(readersToClose, reader)
	}

	// Prepare Output Handler
	options.OutputHandler = func(filename, localPath string) (string, error) {
		f, err := os.Open(localPath)
		if err != nil {
			return "", err
		}
		defer f.Close()
		// Upload to storage
		// We can return the path/id
		return store.Put(f, filename)
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
    var files map[string]string

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
