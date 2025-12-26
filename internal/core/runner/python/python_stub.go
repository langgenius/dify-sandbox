//go:build !linux

package python

import (
	"context"
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner"
	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/utils/log"
)

type PythonRunner struct {
	runner.TempDirRunner
}

func (p *PythonRunner) Run(
	ctx context.Context,
	code string,
	timeout time.Duration,
	stdin []byte,
	preload string,
	options *types.RunnerOptions,
) (chan []byte, chan []byte, chan bool, error) {
	log.Error("Python native runner is only supported on Linux. Please configure sandbox_backend to 'microsandbox' in config.yaml")
	return nil, nil, nil, nil
}

func (p *PythonRunner) InitializeEnvironment(code string, preload string, options *types.RunnerOptions) (string, string, error) {
	log.Error("Python native runner is only supported on Linux. Please configure sandbox_backend to 'microsandbox' in config.yaml")
	return "", "", nil
}
