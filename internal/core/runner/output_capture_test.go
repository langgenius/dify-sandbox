package runner

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type capturedOutput struct {
	stdout    string
	stderr    string
	execError string
	exitCode  int
}

func TestCaptureOutputSeparatesStdoutAndStderr(t *testing.T) {
	r := NewOutputCaptureRunner()
	cmd := exec.Command("/bin/sh", "-c", "printf 'hello\\n'; printf 'warn\\n' >&2")

	if err := r.CaptureOutput(context.Background(), cmd); err != nil {
		t.Fatalf("capture output failed: %v", err)
	}

	output := collectCapturedOutput(r.Result())

	if output.stdout != "hello\n" {
		t.Fatalf("expected stdout to be captured separately, got %q", output.stdout)
	}

	if output.stderr != "warn\n" {
		t.Fatalf("expected stderr to be captured separately, got %q", output.stderr)
	}

	if output.execError != "" {
		t.Fatalf("expected no execution error, got %q", output.execError)
	}

	if output.exitCode != 0 {
		t.Fatalf("expected zero exit code, got %d", output.exitCode)
	}
}

func TestCaptureOutputTracksNonZeroExitCodeWithoutDroppingStderr(t *testing.T) {
	r := NewOutputCaptureRunner()
	cmd := exec.Command("/bin/sh", "-c", "printf 'boom\\n' >&2; exit 7")

	if err := r.CaptureOutput(context.Background(), cmd); err != nil {
		t.Fatalf("capture output failed: %v", err)
	}

	output := collectCapturedOutput(r.Result())

	if output.stderr != "boom\n" {
		t.Fatalf("expected stderr to be preserved, got %q", output.stderr)
	}

	if output.exitCode != 7 {
		t.Fatalf("expected exit code 7, got %d", output.exitCode)
	}

	if output.execError != "" {
		t.Fatalf("expected no synthetic exec error for plain non-zero exit, got %q", output.execError)
	}
}

func TestCaptureOutputReportsTimeoutAsExecutionError(t *testing.T) {
	r := NewOutputCaptureRunner()
	r.SetTimeout(50 * time.Millisecond)
	cmd := exec.Command("/bin/sh", "-c", "sleep 1")

	if err := r.CaptureOutput(context.Background(), cmd); err != nil {
		t.Fatalf("capture output failed: %v", err)
	}

	output := collectCapturedOutput(r.Result())

	if !strings.Contains(output.execError, "error: timeout") {
		t.Fatalf("expected timeout execution error, got %q", output.execError)
	}

	if output.exitCode != -1 {
		t.Fatalf("expected timeout exit code -1, got %d", output.exitCode)
	}
}

func collectCapturedOutput(result *OutputCaptureResult) capturedOutput {
	var output capturedOutput

	for {
		select {
		case <-result.GetDone():
			for {
				select {
				case chunk := <-result.GetStdout():
					output.stdout += string(chunk)
				case chunk := <-result.GetStderr():
					output.stderr += string(chunk)
				case chunk := <-result.GetExecError():
					output.execError += string(chunk)
				default:
					output.exitCode = result.GetExitCode()
					return output
				}
			}
		case chunk := <-result.GetStdout():
			output.stdout += string(chunk)
		case chunk := <-result.GetStderr():
			output.stderr += string(chunk)
		case chunk := <-result.GetExecError():
			output.execError += string(chunk)
		}
	}
}
