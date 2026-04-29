package service

import (
	"errors"

	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/pool"
	"github.com/langgenius/dify-sandbox/internal/static"
	service_types "github.com/langgenius/dify-sandbox/internal/types"
)

var (
	ErrNetworkDisabled = errors.New("network is disabled, please enable it in the configuration")
)

func checkOptions(options *types.RunnerOptions) error {
	configuration := static.GetDifySandboxGlobalConfigurations()

	if options.EnableNetwork && !configuration.EnableNetwork {
		return ErrNetworkDisabled
	}

	return nil
}

// drainChannels collects stdout/stderr from a fork-mode runner and returns
// the consolidated response.
func drainChannels(stdout, stderr <-chan []byte, done <-chan bool) *service_types.DifySandboxResponse {
	stdoutStr := ""
	stderrStr := ""

	for {
		select {
		case <-done:
			return service_types.SuccessResponse(&RunCodeResponse{
				Stdout: stdoutStr,
				Stderr: stderrStr,
			})
		case out := <-stdout:
			stdoutStr += string(out)
		case err := <-stderr:
			stderrStr += string(err)
		}
	}
}

// drainPoolResult collects stdout/stderr from a pool.PoolResult.
func drainPoolResult(result *pool.PoolResult) *service_types.DifySandboxResponse {
	stdoutStr := ""
	stderrStr := ""

	for {
		select {
		case <-result.Done:
			return service_types.SuccessResponse(&RunCodeResponse{
				Stdout: stdoutStr,
				Stderr: stderrStr,
			})
		case out, ok := <-result.Stdout:
			if ok {
				stdoutStr += string(out)
			}
		case err, ok := <-result.Stderr:
			if ok {
				stderrStr += string(err)
			}
		}
	}
}

