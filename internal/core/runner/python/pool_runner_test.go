//go:build integration

package python

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/langgenius/dify-sandbox/internal/pool"
	"github.com/langgenius/dify-sandbox/internal/static"
)

// initTestConfig sets minimal config needed for pool runner tests.
func initTestConfig(t *testing.T) {
	t.Helper()
	if err := static.InitConfig("../../../../conf/config.yaml"); err != nil {
		t.Skipf("cannot load config (not in project root?): %v", err)
	}
}

// skipIfNoLib skips the test when python.so is absent (macOS / CI without Linux build).
func skipIfNoLib(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(LIB_PATH + "/" + "python.so"); err != nil {
		t.Skip("python.so not present, skipping integration test")
	}
}

// ---------------------------------------------------------------------------
// PythonPoolExecutor tests
// ---------------------------------------------------------------------------

func TestPythonPoolExecutor_BasicExecution(t *testing.T) {
	initTestConfig(t)
	skipIfNoLib(t)

	exec := NewPythonPoolExecutor(1)
	defer exec.Shutdown()

	result := exec.Execute(&pool.PoolTask{
		Type:    pool.TaskTypePython,
		Code:    "print('hello from pool')",
		Timeout: 10 * time.Second,
		Options: &pool.RunnerOptions{},
	})

	if result.Error != nil {
		t.Fatalf("Execute returned error: %v", result.Error)
	}

	out := drainStdout(result)
	if !strings.Contains(out, "hello from pool") {
		t.Errorf("unexpected stdout: %q", out)
	}
}

func TestPythonPoolExecutor_StderrCapture(t *testing.T) {
	initTestConfig(t)
	skipIfNoLib(t)

	exec := NewPythonPoolExecutor(1)
	defer exec.Shutdown()

	result := exec.Execute(&pool.PoolTask{
		Type:    pool.TaskTypePython,
		Code:    "import sys\nsys.stderr.write('err line\\n')",
		Timeout: 10 * time.Second,
		Options: &pool.RunnerOptions{},
	})

	if result.Error != nil {
		t.Fatalf("Execute returned error: %v", result.Error)
	}

	errOut := drainStderr(result)
	if !strings.Contains(errOut, "err line") {
		t.Errorf("expected stderr to contain 'err line', got: %q", errOut)
	}
}

func TestPythonPoolExecutor_SyntaxError(t *testing.T) {
	initTestConfig(t)
	skipIfNoLib(t)

	exec := NewPythonPoolExecutor(1)
	defer exec.Shutdown()

	result := exec.Execute(&pool.PoolTask{
		Type:    pool.TaskTypePython,
		Code:    "def broken(:\n    pass",
		Timeout: 10 * time.Second,
		Options: &pool.RunnerOptions{},
	})

	if result.Error != nil {
		t.Fatalf("Execute returned transport error: %v", result.Error)
	}

	errOut := drainStderr(result)
	if errOut == "" {
		t.Error("expected stderr output for syntax error, got nothing")
	}
}

func TestPythonPoolExecutor_Preload(t *testing.T) {
	initTestConfig(t)
	skipIfNoLib(t)

	exec := NewPythonPoolExecutor(1)
	defer exec.Shutdown()

	result := exec.Execute(&pool.PoolTask{
		Type:    pool.TaskTypePython,
		Code:    "print(PRELOADED_VAR)",
		Preload: "PRELOADED_VAR = 'from preload'",
		Timeout: 10 * time.Second,
		Options: &pool.RunnerOptions{},
	})

	if result.Error != nil {
		t.Fatalf("Execute returned error: %v", result.Error)
	}

	out := drainStdout(result)
	if !strings.Contains(out, "from preload") {
		t.Errorf("expected preload variable in output, got: %q", out)
	}
}

func TestPythonPoolExecutor_ProcessReuse(t *testing.T) {
	initTestConfig(t)
	skipIfNoLib(t)

	exec := NewPythonPoolExecutor(1)
	defer exec.Shutdown()

	// Run 5 requests through a single-process pool — all must succeed.
	for i := 0; i < 5; i++ {
		result := exec.Execute(&pool.PoolTask{
			Type:    pool.TaskTypePython,
			Code:    "print('iteration')",
			Timeout: 10 * time.Second,
			Options: &pool.RunnerOptions{},
		})
		if result.Error != nil {
			t.Fatalf("iteration %d: Execute error: %v", i, result.Error)
		}
		out := drainStdout(result)
		if !strings.Contains(out, "iteration") {
			t.Errorf("iteration %d: unexpected output: %q", i, out)
		}
	}
}

func TestPythonPoolExecutor_Shutdown(t *testing.T) {
	initTestConfig(t)
	skipIfNoLib(t)

	exec := NewPythonPoolExecutor(1)
	exec.Shutdown() // must not panic or deadlock
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func drainStdout(r *pool.PoolResult) string {
	var out string
	for {
		select {
		case b, ok := <-r.Stdout:
			if !ok {
				return out
			}
			out += string(b)
		case <-r.Done:
			return out
		}
	}
}

func drainStderr(r *pool.PoolResult) string {
	var out string
	for {
		select {
		case b, ok := <-r.Stderr:
			if !ok {
				return out
			}
			out += string(b)
		case <-r.Done:
			return out
		}
	}
}
