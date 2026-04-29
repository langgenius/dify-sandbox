package pool

import "time"

// TaskType identifies which language runtime to use.
type TaskType string

const (
	TaskTypePython TaskType = "python3"
	TaskTypeNodeJS TaskType = "nodejs"
)

// PoolTask is the unit of work submitted to the pool.
type PoolTask struct {
	Type    TaskType
	Code    string
	Preload string
	Timeout time.Duration
	Options *RunnerOptions
}

// RunnerOptions mirrors runner_types.RunnerOptions so the pool package
// stays free of circular imports. The service layer converts between the two.
type RunnerOptions struct {
	EnableNetwork bool
}

// PoolResult carries the output channels returned by the runner.
type PoolResult struct {
	Stdout <-chan []byte
	Stderr <-chan []byte
	Done   <-chan bool
	Error  error
}

// TaskExecutor is the interface that each language runner must implement.
type TaskExecutor interface {
	Execute(task *PoolTask) *PoolResult
	Shutdown()
}
