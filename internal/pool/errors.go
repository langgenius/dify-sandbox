package pool

import "errors"

var (
	ErrPoolStopping       = errors.New("runtime pool is stopping")
	ErrPoolFull           = errors.New("runtime pool queue is full")
	ErrWorkerNotAvailable = errors.New("no available worker for this task type")
	ErrInvalidTaskType    = errors.New("invalid task type")
	ErrTaskTimeout        = errors.New("task execution timeout")
)
