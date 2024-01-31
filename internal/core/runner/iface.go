package runner

import "time"

type Runner interface {
	// Run runs the code and returns the stdout and stderr
	Run(code string, timeout time.Duration, stdin chan []byte) (<-chan []byte, <-chan []byte, error)
}
