package types

import (
	"context"
	"time"
)

// CodeRunner defines the interface for code execution runners
// Both native and microsandbox runners implement this interface
type CodeRunner interface {
	// Run executes code with the given parameters
	// Returns channels for stdout, stderr, and done signal, along with any error
	Run(
		ctx context.Context,
		code string,
		timeout time.Duration,
		stdin []byte,
		preload string,
		options *RunnerOptions,
	) (chan []byte, chan []byte, chan bool, error)
}
