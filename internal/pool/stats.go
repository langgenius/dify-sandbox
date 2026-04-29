package pool

import "time"

// PoolStats holds runtime statistics for the pool.
type PoolStats struct {
	TotalWorkers   int            `json:"total_workers"`
	ActiveWorkers  int            `json:"active_workers"`
	QueueSize      int            `json:"queue_size"`
	WorkersByType  map[string]int `json:"workers_by_type"`
	TasksProcessed int64          `json:"tasks_processed"`
	TasksFailed    int64          `json:"tasks_failed"`
	Uptime         time.Duration  `json:"uptime"`
}
