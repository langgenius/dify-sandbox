//go:build integration

package nodejs

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/langgenius/dify-sandbox/internal/pool"
	"github.com/langgenius/dify-sandbox/internal/static"
)

func initTestConfig(t *testing.T) {
	t.Helper()
	if err := static.InitConfig("../../../../conf/config.yaml"); err != nil {
		t.Skipf("cannot load config: %v", err)
	}
}

func skipIfNoLib(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(LIB_PATH + "/" + LIB_NAME); err != nil {
		t.Skip("nodejs.so not present, skipping integration test")
	}
}

// ---------------------------------------------------------------------------
// NodeJSPoolExecutor tests
// ---------------------------------------------------------------------------

func TestNodeJSPoolExecutor_BasicExecution(t *testing.T) {
	initTestConfig(t)
	skipIfNoLib(t)

	exec := NewNodeJSPoolExecutor(1)
	defer exec.Shutdown()

	result := exec.Execute(&pool.PoolTask{
		Type:    pool.TaskTypeNodeJS,
		Code:    "console.log('hello from node pool')",
		Timeout: 10 * time.Second,
		Options: &pool.RunnerOptions{},
	})

	if result.Error != nil {
		t.Fatalf("Execute error: %v", result.Error)
	}

	out := drainResult(result)
	if !strings.Contains(out.stdout, "hello from node pool") {
		t.Errorf("unexpected stdout: %q", out.stdout)
	}
}

func TestNodeJSPoolExecutor_ArithmeticResult(t *testing.T) {
	initTestConfig(t)
	skipIfNoLib(t)

	exec := NewNodeJSPoolExecutor(1)
	defer exec.Shutdown()

	result := exec.Execute(&pool.PoolTask{
		Type:    pool.TaskTypeNodeJS,
		Code:    "console.log(String(1 + 2 + 3))",
		Timeout: 10 * time.Second,
		Options: &pool.RunnerOptions{},
	})

	if result.Error != nil {
		t.Fatalf("Execute error: %v", result.Error)
	}

	out := drainResult(result)
	if !strings.Contains(out.stdout, "6") {
		t.Errorf("expected '6' in output, got: %q", out.stdout)
	}
}

func TestNodeJSPoolExecutor_ErrorInCode(t *testing.T) {
	initTestConfig(t)
	skipIfNoLib(t)

	exec := NewNodeJSPoolExecutor(1)
	defer exec.Shutdown()

	result := exec.Execute(&pool.PoolTask{
		Type:    pool.TaskTypeNodeJS,
		Code:    "throw new Error('intentional error')",
		Timeout: 10 * time.Second,
		Options: &pool.RunnerOptions{},
	})

	if result.Error != nil {
		t.Fatalf("transport error: %v", result.Error)
	}

	out := drainResult(result)
	if out.stderr == "" {
		t.Error("expected stderr output for thrown error, got nothing")
	}
}

func TestNodeJSPoolExecutor_Preload(t *testing.T) {
	initTestConfig(t)
	skipIfNoLib(t)

	exec := NewNodeJSPoolExecutor(1)
	defer exec.Shutdown()

	result := exec.Execute(&pool.PoolTask{
		Type:    pool.TaskTypeNodeJS,
		Code:    "console.log(PRELOADED)",
		Preload: "const PRELOADED = 'from preload';",
		Timeout: 10 * time.Second,
		Options: &pool.RunnerOptions{},
	})

	if result.Error != nil {
		t.Fatalf("Execute error: %v", result.Error)
	}

	out := drainResult(result)
	if !strings.Contains(out.stdout, "from preload") {
		t.Errorf("expected preload value in stdout, got: %q", out.stdout)
	}
}

func TestNodeJSPoolExecutor_ProcessReuse(t *testing.T) {
	initTestConfig(t)
	skipIfNoLib(t)

	exec := NewNodeJSPoolExecutor(1)
	defer exec.Shutdown()

	for i := 0; i < 5; i++ {
		result := exec.Execute(&pool.PoolTask{
			Type:    pool.TaskTypeNodeJS,
			Code:    "console.log('iter')",
			Timeout: 10 * time.Second,
			Options: &pool.RunnerOptions{},
		})
		if result.Error != nil {
			t.Fatalf("iteration %d error: %v", i, result.Error)
		}
		out := drainResult(result)
		if !strings.Contains(out.stdout, "iter") {
			t.Errorf("iteration %d unexpected output: %q", i, out.stdout)
		}
	}
}

func TestNodeJSPoolExecutor_Shutdown(t *testing.T) {
	initTestConfig(t)
	skipIfNoLib(t)

	exec := NewNodeJSPoolExecutor(1)
	exec.Shutdown() // must not panic
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type combinedOutput struct {
	stdout string
	stderr string
}

func drainResult(r *pool.PoolResult) combinedOutput {
	var out combinedOutput
	for {
		select {
		case b, ok := <-r.Stdout:
			if ok {
				out.stdout += string(b)
			}
		case b, ok := <-r.Stderr:
			if ok {
				out.stderr += string(b)
			}
		case <-r.Done:
			return out
		}
	}
}
