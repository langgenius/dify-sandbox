package python

import (
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner"
)

type PythonRunner struct {
	runner.Runner
	runner.SeccompRunner
}

func (p *PythonRunner) Run(code string, timeout time.Duration, stdin chan []byte) (<-chan []byte, <-chan []byte, error) {
	return nil, nil, nil
}
