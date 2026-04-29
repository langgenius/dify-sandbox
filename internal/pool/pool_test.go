package pool

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// mockExecutor is a test double for TaskExecutor.
// It runs fn(task) and returns the resulting PoolResult.
// If fn is nil it returns an empty success result.
type mockExecutor struct {
	fn       func(*PoolTask) *PoolResult
	shutdown atomic.Bool
}

func (m *mockExecutor) Execute(task *PoolTask) *PoolResult {
	if m.fn != nil {
		return m.fn(task)
	}
	stdout := make(chan []byte, 1)
	stderr := make(chan []byte, 1)
	done := make(chan bool, 1)
	close(stdout)
	close(stderr)
	done <- true
	close(done)
	return &PoolResult{Stdout: stdout, Stderr: stderr, Done: done}
}

func (m *mockExecutor) Shutdown() {
	m.shutdown.Store(true)
}

// successResult returns a PoolResult whose channels emit one stdout message.
func successResult(msg string) *PoolResult {
	stdout := make(chan []byte, 1)
	stderr := make(chan []byte, 1)
	done := make(chan bool, 1)
	stdout <- []byte(msg)
	close(stdout)
	close(stderr)
	done <- true
	close(done)
	return &PoolResult{Stdout: stdout, Stderr: stderr, Done: done}
}

// errorResult returns a PoolResult with Error set.
func errorResult(err error) *PoolResult {
	return &PoolResult{Error: err}
}

// ---------------------------------------------------------------------------
// Config tests
// ---------------------------------------------------------------------------

func TestDefaultPoolConfig(t *testing.T) {
	cfg := DefaultPoolConfig()
	if cfg.Enabled {
		t.Error("default pool should be disabled")
	}
	if cfg.MaxQueueSize <= 0 {
		t.Error("MaxQueueSize must be > 0")
	}
	if cfg.WorkerIdleTime <= 0 {
		t.Error("WorkerIdleTime must be > 0")
	}
	if cfg.PythonWorkers == nil || cfg.PythonWorkers.WorkerCount <= 0 {
		t.Error("default Python worker count must be > 0")
	}
	if cfg.NodeJSWorkers == nil || cfg.NodeJSWorkers.WorkerCount <= 0 {
		t.Error("default NodeJS worker count must be > 0")
	}
}

func TestPoolConfigValidate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     PoolConfig
		wantErr bool
	}{
		{
			name:    "disabled skips validation",
			cfg:     PoolConfig{Enabled: false},
			wantErr: false,
		},
		{
			name: "valid enabled config",
			cfg: PoolConfig{
				Enabled:        true,
				MaxQueueSize:   100,
				WorkerIdleTime: time.Minute,
			},
			wantErr: false,
		},
		{
			name: "zero queue size",
			cfg: PoolConfig{
				Enabled:        true,
				MaxQueueSize:   0,
				WorkerIdleTime: time.Minute,
			},
			wantErr: true,
		},
		{
			name: "zero idle time",
			cfg: PoolConfig{
				Enabled:        true,
				MaxQueueSize:   100,
				WorkerIdleTime: 0,
			},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr && err == nil {
				t.Error("expected error but got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestPoolConfigWorkerCount(t *testing.T) {
	cfg := PoolConfig{
		PythonWorkers: &LanguageConfig{WorkerCount: 7},
		NodeJSWorkers: &LanguageConfig{WorkerCount: 3},
	}
	if cfg.WorkerCount(TaskTypePython) != 7 {
		t.Errorf("expected 7, got %d", cfg.WorkerCount(TaskTypePython))
	}
	if cfg.WorkerCount(TaskTypeNodeJS) != 3 {
		t.Errorf("expected 3, got %d", cfg.WorkerCount(TaskTypeNodeJS))
	}
	if cfg.WorkerCount("unknown") != 0 {
		t.Error("expected 0 for unknown task type")
	}
}

// ---------------------------------------------------------------------------
// Pool lifecycle tests
// ---------------------------------------------------------------------------

func newTestPool() *RuntimePool {
	cfg := DefaultPoolConfig()
	cfg.EnableMonitor = false
	return NewRuntimePool(cfg)
}

func TestNewRuntimePool(t *testing.T) {
	p := newTestPool()
	if p == nil {
		t.Fatal("NewRuntimePool returned nil")
	}
	p.Shutdown()
}

func TestRegisterAndSubmit(t *testing.T) {
	p := newTestPool()
	defer p.Shutdown()

	exec := &mockExecutor{fn: func(task *PoolTask) *PoolResult {
		return successResult("hello pool")
	}}
	p.RegisterExecutor(TaskTypePython, exec)

	result, err := p.Submit(&PoolTask{Type: TaskTypePython, Code: "print('hi')"})
	if err != nil {
		t.Fatalf("Submit returned error: %v", err)
	}

	var out string
	for line := range result.Stdout {
		out += string(line)
	}
	if out != "hello pool" {
		t.Errorf("unexpected stdout: %q", out)
	}
}

func TestSubmitUnknownType(t *testing.T) {
	p := newTestPool()
	defer p.Shutdown()

	_, err := p.Submit(&PoolTask{Type: "ruby"})
	if !errors.Is(err, ErrWorkerNotAvailable) {
		t.Errorf("expected ErrWorkerNotAvailable, got %v", err)
	}
}

func TestSubmitAfterShutdown(t *testing.T) {
	p := newTestPool()
	p.RegisterExecutor(TaskTypePython, &mockExecutor{})
	p.Shutdown()

	_, err := p.Submit(&PoolTask{Type: TaskTypePython})
	if !errors.Is(err, ErrPoolStopping) {
		t.Errorf("expected ErrPoolStopping, got %v", err)
	}
}

func TestShutdownCallsExecutorShutdown(t *testing.T) {
	p := newTestPool()
	exec := &mockExecutor{}
	p.RegisterExecutor(TaskTypePython, exec)
	p.Shutdown()

	if !exec.shutdown.Load() {
		t.Error("executor.Shutdown() was not called")
	}
}

func TestGetStats(t *testing.T) {
	p := newTestPool()
	defer p.Shutdown()

	p.RegisterExecutor(TaskTypePython, &mockExecutor{})
	p.RegisterExecutor(TaskTypeNodeJS, &mockExecutor{})

	stats := p.GetStats()
	if stats.TotalWorkers != 2 {
		t.Errorf("expected 2 workers, got %d", stats.TotalWorkers)
	}
	if _, ok := stats.WorkersByType[string(TaskTypePython)]; !ok {
		t.Error("missing python entry in WorkersByType")
	}
}

func TestStatsTrackProcessedAndFailed(t *testing.T) {
	p := newTestPool()
	defer p.Shutdown()

	var callCount int32
	exec := &mockExecutor{fn: func(task *PoolTask) *PoolResult {
		n := atomic.AddInt32(&callCount, 1)
		if n%2 == 0 {
			return errorResult(errors.New("even call fails"))
		}
		return successResult("ok")
	}}
	p.RegisterExecutor(TaskTypePython, exec)

	for i := 0; i < 4; i++ {
		p.Submit(&PoolTask{Type: TaskTypePython}) //nolint:errcheck
	}

	stats := p.GetStats()
	if stats.TasksProcessed != 4 {
		t.Errorf("expected 4 processed, got %d", stats.TasksProcessed)
	}
	if stats.TasksFailed != 2 {
		t.Errorf("expected 2 failed, got %d", stats.TasksFailed)
	}
}

// ---------------------------------------------------------------------------
// Concurrency tests
// ---------------------------------------------------------------------------

func TestConcurrentSubmits(t *testing.T) {
	p := newTestPool()
	defer p.Shutdown()

	p.RegisterExecutor(TaskTypePython, &mockExecutor{})

	const goroutines = 20
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := p.Submit(&PoolTask{Type: TaskTypePython, Code: "x=1"})
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent submit error: %v", err)
	}
}

func TestConcurrentRegisterAndSubmit(t *testing.T) {
	p := newTestPool()
	defer p.Shutdown()

	var wg sync.WaitGroup

	// Register while submitting — must not race.
	wg.Add(1)
	go func() {
		defer wg.Done()
		p.RegisterExecutor(TaskTypeNodeJS, &mockExecutor{})
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		// May get ErrWorkerNotAvailable if register hasn't happened yet; that's fine.
		p.Submit(&PoolTask{Type: TaskTypeNodeJS}) //nolint:errcheck
	}()

	wg.Wait()
}

// ---------------------------------------------------------------------------
// Task / RunnerOptions tests
// ---------------------------------------------------------------------------

func TestTaskTypes(t *testing.T) {
	if TaskTypePython != "python3" {
		t.Errorf("unexpected python task type: %s", TaskTypePython)
	}
	if TaskTypeNodeJS != "nodejs" {
		t.Errorf("unexpected nodejs task type: %s", TaskTypeNodeJS)
	}
}

func TestPoolResultZeroValue(t *testing.T) {
	var r PoolResult
	if r.Error != nil {
		t.Error("zero PoolResult should have nil Error")
	}
	if r.Stdout != nil || r.Stderr != nil || r.Done != nil {
		t.Error("zero PoolResult channels should be nil")
	}
}
