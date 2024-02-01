package python

import (
	_ "embed"
	"fmt"
	"syscall"
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner"
)

type PythonRunner struct {
	runner.Runner
	runner.SeccompRunner
}

//go:embed prescript.py
var python_sandbox_fs []byte

func (p *PythonRunner) Run(code string, timeout time.Duration, stdin chan []byte) (<-chan []byte, <-chan []byte, error) {
	err := p.WithSeccomp(func() error {
		syscall.Exec("/usr/bin/python3", []string{"/usr/bin/python3", "./internal/core/runner/python/prescript.py"}, nil)
		return nil
	})
	if err != nil {
		fmt.Println(err)
	}
	return nil, nil, nil
}
