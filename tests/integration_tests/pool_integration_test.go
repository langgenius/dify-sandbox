//go:build integration

package integrationtests_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	runner_types "github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/service"
	"github.com/langgenius/dify-sandbox/internal/static"
	"github.com/langgenius/dify-sandbox/internal/types"
)

// initPoolConfig enables pool mode and initialises the pool.
// Skips if the config cannot be loaded (e.g. no Linux .so files).
func initPoolConfig(t *testing.T) {
	t.Helper()
	if err := static.InitConfig("../../conf/config.yaml"); err != nil {
		t.Skipf("pool test: cannot load config: %v", err)
	}

	// Force pool mode on.
	cfg := static.GetDifySandboxGlobalConfigurations()
	if !cfg.WorkerPool.Enabled {
		t.Skip("pool test: worker_pool.enabled is false – set WORKER_POOL_ENABLED=true to run")
	}

	service.InitPool()
	t.Cleanup(service.ShutdownPool)
}

// assertPoolSuccess is a test helper that checks a pool-mode response.
func assertPoolSuccess(t *testing.T, resp *types.DifySandboxResponse, wantStdout string) {
	t.Helper()
	if resp.Code != 0 {
		t.Fatalf("pool: non-zero response code %d: %v", resp.Code, resp)
	}
	data, ok := resp.Data.(*service.RunCodeResponse)
	if !ok {
		t.Fatalf("pool: unexpected data type %T", resp.Data)
	}
	if data.Stderr != "" {
		t.Errorf("pool: unexpected stderr: %s", data.Stderr)
	}
	if wantStdout != "" && !strings.Contains(data.Stdout, wantStdout) {
		t.Errorf("pool: stdout %q does not contain %q", data.Stdout, wantStdout)
	}
}

// ---------------------------------------------------------------------------
// Python pool integration tests
// ---------------------------------------------------------------------------

func TestPoolPythonBasic(t *testing.T) {
	initPoolConfig(t)

	resp := service.RunPython3Code(
		`print("pool python basic")`,
		"",
		&runner_types.RunnerOptions{EnableNetwork: false},
	)
	assertPoolSuccess(t, resp, "pool python basic")
}

func TestPoolPythonJSON(t *testing.T) {
	initPoolConfig(t)

	resp := service.RunPython3Code(
		`import json; print(json.dumps({"k": "v"}))`,
		"",
		&runner_types.RunnerOptions{EnableNetwork: false},
	)
	assertPoolSuccess(t, resp, `{"k": "v"}`)
}

func TestPoolPythonArithmetic(t *testing.T) {
	initPoolConfig(t)

	resp := service.RunPython3Code(
		`print(2 ** 10)`,
		"",
		&runner_types.RunnerOptions{EnableNetwork: false},
	)
	assertPoolSuccess(t, resp, "1024")
}

func TestPoolPythonPreload(t *testing.T) {
	initPoolConfig(t)

	resp := service.RunPython3Code(
		`print(ANSWER)`,
		`ANSWER = 42`,
		&runner_types.RunnerOptions{EnableNetwork: false},
	)
	assertPoolSuccess(t, resp, "42")
}

func TestPoolPythonSyntaxError(t *testing.T) {
	initPoolConfig(t)

	resp := service.RunPython3Code(
		`def bad(:\n    pass`,
		"",
		&runner_types.RunnerOptions{EnableNetwork: false},
	)
	if resp.Code != 0 {
		return // error correctly propagated
	}
	data := resp.Data.(*service.RunCodeResponse)
	if data.Stderr == "" {
		t.Error("expected stderr for syntax error")
	}
}

func TestPoolPythonConcurrent(t *testing.T) {
	initPoolConfig(t)

	const n = 10
	var wg sync.WaitGroup
	errs := make(chan string, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			resp := service.RunPython3Code(
				`print("concurrent")`,
				"",
				&runner_types.RunnerOptions{EnableNetwork: false},
			)
			if resp.Code != 0 {
				errs <- resp.Message
				return
			}
			data := resp.Data.(*service.RunCodeResponse)
			if !strings.Contains(data.Stdout, "concurrent") {
				errs <- data.Stdout
			}
		}(i)
	}

	wg.Wait()
	close(errs)
	for e := range errs {
		t.Errorf("concurrent pool error: %s", e)
	}
}

func TestPoolPythonProcessReuse(t *testing.T) {
	initPoolConfig(t)

	// Run more requests than pool workers to exercise process reuse.
	cfg := static.GetDifySandboxGlobalConfigurations()
	iterations := cfg.WorkerPool.Python*3 + 1

	for i := 0; i < iterations; i++ {
		resp := service.RunPython3Code(
			`print("reuse")`,
			"",
			&runner_types.RunnerOptions{EnableNetwork: false},
		)
		assertPoolSuccess(t, resp, "reuse")
	}
}

// ---------------------------------------------------------------------------
// Node.js pool integration tests
// ---------------------------------------------------------------------------

func TestPoolNodeJSBasic(t *testing.T) {
	initPoolConfig(t)

	resp := service.RunNodeJsCode(
		`console.log("pool nodejs basic")`,
		"",
		&runner_types.RunnerOptions{EnableNetwork: false},
	)
	assertPoolSuccess(t, resp, "pool nodejs basic")
}

func TestPoolNodeJSArithmetic(t *testing.T) {
	initPoolConfig(t)

	resp := service.RunNodeJsCode(
		`console.log(String(6 * 7))`,
		"",
		&runner_types.RunnerOptions{EnableNetwork: false},
	)
	assertPoolSuccess(t, resp, "42")
}

func TestPoolNodeJSPreload(t *testing.T) {
	initPoolConfig(t)

	resp := service.RunNodeJsCode(
		`console.log(GREETING)`,
		`const GREETING = "hello preload";`,
		&runner_types.RunnerOptions{EnableNetwork: false},
	)
	assertPoolSuccess(t, resp, "hello preload")
}

func TestPoolNodeJSErrorPropagation(t *testing.T) {
	initPoolConfig(t)

	resp := service.RunNodeJsCode(
		`throw new Error("intentional")`,
		"",
		&runner_types.RunnerOptions{EnableNetwork: false},
	)
	if resp.Code != 0 {
		return // error correctly propagated
	}
	data := resp.Data.(*service.RunCodeResponse)
	if data.Stderr == "" {
		t.Error("expected stderr for thrown error")
	}
}

func TestPoolNodeJSConcurrent(t *testing.T) {
	initPoolConfig(t)

	const n = 8
	var wg sync.WaitGroup
	errs := make(chan string, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp := service.RunNodeJsCode(
				`console.log("concurrent node")`,
				"",
				&runner_types.RunnerOptions{EnableNetwork: false},
			)
			if resp.Code != 0 {
				errs <- resp.Message
				return
			}
			data := resp.Data.(*service.RunCodeResponse)
			if !strings.Contains(data.Stdout, "concurrent node") {
				errs <- data.Stdout
			}
		}()
	}

	wg.Wait()
	close(errs)
	for e := range errs {
		t.Errorf("concurrent node pool error: %s", e)
	}
}

// ---------------------------------------------------------------------------
// Pool vs fork mode parity test
// ---------------------------------------------------------------------------

// TestPoolParityWithForkMode runs the same Python snippet in both modes and
// compares the stdout output.
func TestPoolParityWithForkMode(t *testing.T) {
	if err := static.InitConfig("../../conf/config.yaml"); err != nil {
		t.Skipf("cannot load config: %v", err)
	}

	code := `import math; print(math.pi)`
	opts := &runner_types.RunnerOptions{EnableNetwork: false}

	// Fork mode (globalPool == nil at this point).
	forkResp := service.RunPython3Code(code, "", opts)
	if forkResp.Code != 0 {
		t.Skipf("fork mode unavailable: %v", forkResp)
	}
	forkOut := strings.TrimSpace(forkResp.Data.(*service.RunCodeResponse).Stdout)

	// Pool mode.
	cfg := static.GetDifySandboxGlobalConfigurations()
	if !cfg.WorkerPool.Enabled {
		t.Skip("worker_pool.enabled is false")
	}
	service.InitPool()
	defer service.ShutdownPool()

	poolResp := service.RunPython3Code(code, "", opts)
	if poolResp.Code != 0 {
		t.Fatalf("pool mode failed: %v", poolResp)
	}
	poolOut := strings.TrimSpace(poolResp.Data.(*service.RunCodeResponse).Stdout)

	if forkOut != poolOut {
		t.Errorf("parity mismatch: fork=%q pool=%q", forkOut, poolOut)
	}
}

// ---------------------------------------------------------------------------
// Timeout test
// ---------------------------------------------------------------------------

func TestPoolPythonTimeout(t *testing.T) {
	if err := static.InitConfig("../../conf/config.yaml"); err != nil {
		t.Skipf("cannot load config: %v", err)
	}
	cfg := static.GetDifySandboxGlobalConfigurations()
	if !cfg.WorkerPool.Enabled {
		t.Skip("worker_pool.enabled is false")
	}
	service.InitPool()
	defer service.ShutdownPool()

	start := time.Now()
	resp := service.RunPython3Code(
		`import time; time.sleep(60)`,
		"",
		&runner_types.RunnerOptions{EnableNetwork: false},
	)
	elapsed := time.Since(start)

	// Should return within WorkerTimeout + a few seconds of buffer.
	maxExpected := time.Duration(cfg.WorkerTimeout+5) * time.Second
	if elapsed > maxExpected {
		t.Errorf("timeout took too long: %v (expected < %v)", elapsed, maxExpected)
	}

	// Either an error code or stderr should be present.
	if resp.Code == 0 {
		data := resp.Data.(*service.RunCodeResponse)
		if data.Stderr == "" {
			t.Error("expected timeout error in stderr")
		}
	}
}
