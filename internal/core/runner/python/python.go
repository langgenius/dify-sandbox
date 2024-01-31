package python

import (
	"fmt"
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner"
)

type PythonRunner struct {
	runner.Runner
	runner.SeccompRunner
}

func (p *PythonRunner) Run(code string, timeout time.Duration, stdin chan []byte) (<-chan []byte, <-chan []byte, error) {
	err := p.WithSeccomp(func() error {
		fmt.Println("Hello World")
		return nil
	})
	if err != nil {
		fmt.Println(err)
	}
	return nil, nil, nil
}
