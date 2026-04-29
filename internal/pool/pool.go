package pool

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/langgenius/dify-sandbox/internal/utils/log"
)

// RuntimePool manages a set of per-language worker executors.
type RuntimePool struct {
	config    PoolConfig
	executors map[TaskType]TaskExecutor
	mu        sync.RWMutex
	stopping  atomic.Bool
	createdAt time.Time

	tasksMu        sync.Mutex
	tasksProcessed int64
	tasksFailed    int64
}

// NewRuntimePool creates a pool and starts the optional monitor goroutine.
func NewRuntimePool(config PoolConfig) *RuntimePool {
	p := &RuntimePool{
		config:    config,
		executors: make(map[TaskType]TaskExecutor),
		createdAt: time.Now(),
	}

	if config.EnableMonitor {
		go p.monitor()
	}

	return p
}

// RegisterExecutor registers a TaskExecutor for the given task type.
func (p *RuntimePool) RegisterExecutor(taskType TaskType, executor TaskExecutor) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.executors[taskType] = executor
	log.Info("pool: registered executor for %s", taskType)
}

// Submit executes the task synchronously and returns the result.
func (p *RuntimePool) Submit(task *PoolTask) (*PoolResult, error) {
	if p.stopping.Load() {
		return nil, ErrPoolStopping
	}

	p.mu.RLock()
	executor, ok := p.executors[task.Type]
	p.mu.RUnlock()

	if !ok {
		return nil, ErrWorkerNotAvailable
	}

	result := executor.Execute(task)

	p.tasksMu.Lock()
	p.tasksProcessed++
	if result.Error != nil {
		p.tasksFailed++
	}
	p.tasksMu.Unlock()

	return result, result.Error
}

// Shutdown stops the pool and all registered executors.
func (p *RuntimePool) Shutdown() {
	p.stopping.Store(true)

	p.mu.Lock()
	defer p.mu.Unlock()

	for _, executor := range p.executors {
		executor.Shutdown()
	}

	log.Info("pool: shutdown complete")
}

// GetStats returns a snapshot of pool statistics.
func (p *RuntimePool) GetStats() PoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	p.tasksMu.Lock()
	processed := p.tasksProcessed
	failed := p.tasksFailed
	p.tasksMu.Unlock()

	stats := PoolStats{
		TotalWorkers:   len(p.executors),
		ActiveWorkers:  len(p.executors),
		WorkersByType:  make(map[string]int),
		TasksProcessed: processed,
		TasksFailed:    failed,
		Uptime:         time.Since(p.createdAt),
	}

	for taskType := range p.executors {
		stats.WorkersByType[string(taskType)] = 1
	}

	return stats
}

func (p *RuntimePool) monitor() {
	ticker := time.NewTicker(p.config.MonitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if p.stopping.Load() {
				return
			}
			stats := p.GetStats()
			log.Info("pool stats: workers=%d, processed=%d, failed=%d, uptime=%v",
				stats.TotalWorkers, stats.TasksProcessed, stats.TasksFailed, stats.Uptime)
		}
	}
}
