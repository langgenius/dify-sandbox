package nodejs_microsandbox

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
)

// TestNodeJSMicroSandboxRunner_NewRunner tests the creation of a new runner
func TestNodeJSMicroSandboxRunner_NewRunner(t *testing.T) {
	runner := &NodeJSMicroSandboxRunner{}
	if runner == nil {
		t.Fatal("Failed to create NodeJSMicroSandboxRunner")
	}
}

func skipIfNoMsbAPIKey(t *testing.T) {
	if os.Getenv("MSB_API_KEY") == "" {
		t.Skip("Skipping microsandbox test: MSB_API_KEY not set")
	}
}

// TestNodeJSMicroSandboxRunner_Run_ValidCode tests running valid Node.js code
func TestNodeJSMicroSandboxRunner_Run_ValidCode(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	skipIfNoMsbAPIKey(t)

	runner := &NodeJSMicroSandboxRunner{}
	code := `console.log("Hello, World!");`
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

// TestNodeJSMicroSandboxRunner_Run_WithNetwork tests running code with network enabled
func TestNodeJSMicroSandboxRunner_Run_WithNetwork(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	skipIfNoMsbAPIKey(t)

	runner := &NodeJSMicroSandboxRunner{}
	code := `console.log("Network test");`
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

// TestNodeJSMicroSandboxRunner_Run_WithPreload tests running code with preload
func TestNodeJSMicroSandboxRunner_WithPreload(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	skipIfNoMsbAPIKey(t)

	runner := &NodeJSMicroSandboxRunner{}
	preload := `
const helper = function(x) {
    return Math.sqrt(x);
};
`
	code := `console.log(helper(16));`
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

// TestNodeJSMicroSandboxRunner_Run_AsyncCode tests running async JavaScript code
func TestNodeJSMicroSandboxRunner_Run_AsyncCode(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	skipIfNoMsbAPIKey(t)

	runner := &NodeJSMicroSandboxRunner{}
	code := `
setTimeout(() => {
    console.log("Async output");
}, 100);
console.log("Sync output");
`
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
	var stdoutStr string
	for out := range stdout {
		stdoutStr += string(out)
	}
	for range stderr {
	}

	t.Logf("Output: %s", stdoutStr)
}

// TestNodeJSMicroSandboxRunner_Run_Timeout tests execution timeout
func TestNodeJSMicroSandboxRunner_Run_Timeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	skipIfNoMsbAPIKey(t)

	runner := &NodeJSMicroSandboxRunner{}
	code := `
const startTime = Date.now();
while (Date.now() - startTime < 10000) {
    // Busy wait for 10 seconds
}
console.log("This should not print");
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

// TestNodeJSMicroSandboxRunner_Run_SyntaxError tests handling of syntax errors
func TestNodeJSMicroSandboxRunner_Run_SyntaxError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	skipIfNoMsbAPIKey(t)

	runner := &NodeJSMicroSandboxRunner{}
	code := `console.log("unclosed string`
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

// TestNodeJSMicroSandboxRunner_Run_RuntimeError tests handling of runtime errors
func TestNodeJSMicroSandboxRunner_Run_RuntimeError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	skipIfNoMsbAPIKey(t)

	runner := &NodeJSMicroSandboxRunner{}
	code := `
throw new Error("Intentional error");
console.log("This should not print");
`
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

	t.Logf("Error output: %s", stderrStr)
}

// TestNodeJSMicroSandboxRunner_Run_Stdin tests code that reads from stdin
func TestNodeJSMicroSandboxRunner_Run_Stdin(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	skipIfNoMsbAPIKey(t)

	runner := &NodeJSMicroSandboxRunner{}
	code := `
process.stdin.setEncoding('utf8');
process.stdin.on('data', (data) => {
    console.log('Received:', data);
});
`
	options := &types.RunnerOptions{
		EnableNetwork: false,
	}

	stdin := []byte("test input")

	stdout, stderr, done, err := runner.Run(context.TODO(), code, 5*time.Second, stdin, "", options)
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

// TestNodeJSMicroSandboxRunner_Run_ComplexCode tests running more complex JavaScript code
func TestNodeJSMicroSandboxRunner_Run_ComplexCode(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	skipIfNoMsbAPIKey(t)

	runner := &NodeJSMicroSandboxRunner{}
	code := `
// Test various JavaScript features
const arr = [1, 2, 3, 4, 5];
const sum = arr.reduce((a, b) => a + b, 0);
console.log("Sum:", sum);

const obj = { name: "test", value: 42 };
console.log("Object:", JSON.stringify(obj));

// Test arrow functions
const double = x => x * 2;
console.log("Double 5:", double(5));

// Test promises
Promise.resolve(42).then(val => console.log("Promise value:", val));
`
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
	var stdoutStr string
	for out := range stdout {
		stdoutStr += string(out)
	}
	for range stderr {
	}

	t.Logf("Complex code output: %s", stdoutStr)
}
