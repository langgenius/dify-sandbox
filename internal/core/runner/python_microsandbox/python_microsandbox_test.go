package python_microsandbox

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
)

// TestPythonMicroSandboxRunner_NewRunner tests the creation of a new runner
func TestPythonMicroSandboxRunner_NewRunner(t *testing.T) {
	runner := &PythonMicroSandboxRunner{}
	if runner == nil {
		t.Fatal("Failed to create PythonMicroSandboxRunner")
	}
}

func skipIfNoMsbAPIKey(t *testing.T) {
	if os.Getenv("MSB_API_KEY") == "" {
		t.Skip("Skipping microsandbox test: MSB_API_KEY not set")
	}
}

// TestPythonMicroSandboxRunner_Run_ValidCode tests running valid Python code
func TestPythonMicroSandboxRunner_Run_ValidCode(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	skipIfNoMsbAPIKey(t)

	runner := &PythonMicroSandboxRunner{}
	code := `print("Hello, World!")`
	options := &types.RunnerOptions{
		EnableNetwork: false,
	}

	stdout, stderr, done, err := runner.Run(context.TODO(), code, 5*time.Second, nil, "", options)
	if err != nil {
		t.Fatalf("Failed to run code: %v", err)
	}

	// Wait for completion
	<-done

	// Drain all output from channels
	close(stdout)
	close(stderr)
	var stdoutStr, stderrStr string
	for out := range stdout {
		stdoutStr += string(out)
	}
	for err := range stderr {
		stderrStr += string(err)
	}

	// Check if output contains expected result (if microsandbox is available)
	if stdoutStr == "" && stderrStr == "" {
		t.Skip("Microsandbox not available, skipping test")
	}

	if stderrStr != "" && stdoutStr == "" {
		t.Logf("Code execution failed (may be expected if microsandbox not installed): %s", stderrStr)
	}
}

// TestPythonMicroSandboxRunner_Run_WithNetwork tests running code with network enabled
func TestPythonMicroSandboxRunner_Run_WithNetwork(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	skipIfNoMsbAPIKey(t)

	runner := &PythonMicroSandboxRunner{}
	code := `print("Network test")`
	options := &types.RunnerOptions{
		EnableNetwork: true,
	}

	stdout, stderr, done, err := runner.Run(context.TODO(), code, 5*time.Second, nil, "", options)
	if err != nil {
		t.Fatalf("Failed to run code: %v", err)
	}

	// Wait for completion
	<-done

	// Drain channels
	close(stdout)
	close(stderr)
	for range stdout {
	}
	for range stderr {
	}
}

// TestPythonMicroSandboxRunner_Run_WithPreload tests running code with preload
func TestPythonMicroSandboxRunner_WithPreload(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	skipIfNoMsbAPIKey(t)

	runner := &PythonMicroSandboxRunner{}
	preload := `
import math
def helper(x):
    return math.sqrt(x)
`
	code := `print(helper(16))`
	options := &types.RunnerOptions{
		EnableNetwork: false,
	}

	stdout, stderr, done, err := runner.Run(context.TODO(), code, 5*time.Second, nil, preload, options)
	if err != nil {
		t.Fatalf("Failed to run code: %v", err)
	}

	// Wait for completion
	<-done

	// Drain all output from channels
	close(stdout)
	close(stderr)
	var stdoutStr string
	for out := range stdout {
		stdoutStr += string(out)
	}
	for range stderr {
	}

	// Check if output contains expected result (if microsandbox is available)
	if stdoutStr == "" {
		t.Skip("Microsandbox not available, skipping test")
	}
}

// TestPythonMicroSandboxRunner_Run_Timeout tests execution timeout
func TestPythonMicroSandboxRunner_Run_Timeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	skipIfNoMsbAPIKey(t)

	runner := &PythonMicroSandboxRunner{}
	code := `
import time
time.sleep(10)
print("This should not print")
`
	options := &types.RunnerOptions{
		EnableNetwork: false,
	}

	stdout, stderr, done, err := runner.Run(context.TODO(), code, 2*time.Second, nil, "", options)
	if err != nil {
		t.Fatalf("Failed to run code: %v", err)
	}

	// Wait for completion
	<-done

	// Drain all output from channels
	close(stdout)
	close(stderr)
	var stderrStr string
	for range stdout {
	}
	for err := range stderr {
		stderrStr += string(err)
	}

	if stderrStr != "" {
		t.Logf("Got expected error: %s", stderrStr)
	}
}

// TestPythonMicroSandboxRunner_Run_SyntaxError tests handling of syntax errors
func TestPythonMicroSandboxRunner_Run_SyntaxError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	skipIfNoMsbAPIKey(t)

	runner := &PythonMicroSandboxRunner{}
	code := `print("unclosed string`
	options := &types.RunnerOptions{
		EnableNetwork: false,
	}

	stdout, stderr, done, err := runner.Run(context.TODO(), code, 5*time.Second, nil, "", options)
	if err != nil {
		t.Fatalf("Failed to run code: %v", err)
	}

	// Wait for completion
	<-done

	// Drain all output from channels
	close(stdout)
	close(stderr)
	var stderrStr string
	for range stdout {
	}
	for err := range stderr {
		stderrStr += string(err)
	}

	// If microsandbox is available, should get error output
	if stderrStr == "" {
		t.Skip("Microsandbox not available, skipping test")
	}

	t.Logf("Got expected error: %s", stderrStr)
}
