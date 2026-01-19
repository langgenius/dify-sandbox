//go:build !linux

package nodejs

import (
	"context"
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner"
	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/utils/log"
)

type NodeJsRunner struct {
	runner.TempDirRunner
}

func (p *NodeJsRunner) Run(
	ctx context.Context,
	code string,
	timeout time.Duration,
	stdin []byte,
	preload string,
	options *types.RunnerOptions,
) (chan []byte, chan []byte, chan bool, error) {
	log.Error("Node.js native runner is only supported on Linux. Please configure sandbox_backend to 'microsandbox' in config.yaml")
	return nil, nil, nil, nil
}

func (p *NodeJsRunner) InitializeEnvironment(code string, preload string, root_path string) (string, error) {
	log.Error("Node.js native runner is only supported on Linux. Please configure sandbox_backend to 'microsandbox' in config.yaml")
	return "", nil
}
