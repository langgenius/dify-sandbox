package pool

import (
	"errors"
	"time"
)

// PoolConfig holds the configuration for the runtime worker pool.
type PoolConfig struct {
	Enabled         bool          `yaml:"enabled"`
	MaxQueueSize    int           `yaml:"max_queue_size"`
	WorkerIdleTime  time.Duration `yaml:"worker_idle_time"`
	EnableMonitor   bool          `yaml:"enable_monitor"`
	MonitorInterval time.Duration `yaml:"monitor_interval"`

	PythonWorkers *LanguageConfig `yaml:"python_workers,omitempty"`
	NodeJSWorkers *LanguageConfig `yaml:"nodejs_workers,omitempty"`
}

// LanguageConfig holds per-language worker settings.
type LanguageConfig struct {
	Enabled     bool `yaml:"enabled"`
	WorkerCount int  `yaml:"workers"`
}

// DefaultPoolConfig returns a sensible default configuration.
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		Enabled:         false,
		MaxQueueSize:    1000,
		WorkerIdleTime:  5 * time.Minute,
		EnableMonitor:   true,
		MonitorInterval: 30 * time.Second,
		PythonWorkers: &LanguageConfig{
			Enabled:     true,
			WorkerCount: 4,
		},
		NodeJSWorkers: &LanguageConfig{
			Enabled:     true,
			WorkerCount: 2,
		},
	}
}

// Validate returns an error when the configuration is invalid.
func (c *PoolConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.MaxQueueSize <= 0 {
		return errors.New("pool.max_queue_size must be positive")
	}
	if c.WorkerIdleTime <= 0 {
		return errors.New("pool.worker_idle_time must be positive")
	}
	return nil
}

// WorkerCount returns the configured worker count for a language.
func (c *PoolConfig) WorkerCount(lang TaskType) int {
	switch lang {
	case TaskTypePython:
		if c.PythonWorkers != nil {
			return c.PythonWorkers.WorkerCount
		}
	case TaskTypeNodeJS:
		if c.NodeJSWorkers != nil {
			return c.NodeJSWorkers.WorkerCount
		}
	}
	return 0
}
