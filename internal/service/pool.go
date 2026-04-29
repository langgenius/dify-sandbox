package service

import (
	"github.com/langgenius/dify-sandbox/internal/core/runner/nodejs"
	"github.com/langgenius/dify-sandbox/internal/core/runner/python"
	"github.com/langgenius/dify-sandbox/internal/pool"
	"github.com/langgenius/dify-sandbox/internal/static"
	"github.com/langgenius/dify-sandbox/internal/utils/log"
)

var globalPool *pool.RuntimePool

// InitPool initialises the process pool if worker_pool.enabled is true.
// It is safe to call multiple times; subsequent calls are no-ops.
func InitPool() {
	cfg := static.GetDifySandboxGlobalConfigurations()
	if !cfg.WorkerPool.Enabled {
		return
	}
	if globalPool != nil {
		return
	}

	poolCfg := pool.DefaultPoolConfig()
	poolCfg.Enabled = true
	if cfg.WorkerPool.Python > 0 {
		poolCfg.PythonWorkers.WorkerCount = cfg.WorkerPool.Python
	}
	if cfg.WorkerPool.NodeJS > 0 {
		poolCfg.NodeJSWorkers.WorkerCount = cfg.WorkerPool.NodeJS
	}

	globalPool = pool.NewRuntimePool(poolCfg)

	pythonExec := python.NewPythonPoolExecutor(poolCfg.PythonWorkers.WorkerCount)
	globalPool.RegisterExecutor(pool.TaskTypePython, pythonExec)

	nodeExec := nodejs.NewNodeJSPoolExecutor(poolCfg.NodeJSWorkers.WorkerCount)
	globalPool.RegisterExecutor(pool.TaskTypeNodeJS, nodeExec)

	log.Info("worker pool initialised (python_workers=%d, nodejs_workers=%d)",
		poolCfg.PythonWorkers.WorkerCount, poolCfg.NodeJSWorkers.WorkerCount)
}

// ShutdownPool gracefully stops the pool.
func ShutdownPool() {
	if globalPool != nil {
		globalPool.Shutdown()
		globalPool = nil
	}
}
