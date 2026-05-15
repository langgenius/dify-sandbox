package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/langgenius/dify-sandbox/internal/types"
)

type fakeOutputCaptureResult struct {
	stdout    chan []byte
	stderr    chan []byte
	execError chan []byte
	done      chan bool
	exitCode  int
}

func newFakeOutputCaptureResult() *fakeOutputCaptureResult {
	return &fakeOutputCaptureResult{
		stdout:    make(chan []byte),
		stderr:    make(chan []byte),
		execError: make(chan []byte),
		done:      make(chan bool),
	}
}

func (r *fakeOutputCaptureResult) GetStdout() chan []byte {
	return r.stdout
}

func (r *fakeOutputCaptureResult) GetStderr() chan []byte {
	return r.stderr
}

func (r *fakeOutputCaptureResult) GetExecError() chan []byte {
	return r.execError
}

func (r *fakeOutputCaptureResult) GetDone() chan bool {
	return r.done
}

func (r *fakeOutputCaptureResult) GetExitCode() int {
	return r.exitCode
}

func TestCollectRunCodeResponseKeepsSuccessfulStderrOutOfError(t *testing.T) {
	result := newFakeOutputCaptureResult()

	go func() {
		result.GetStdout() <- []byte("hello\n")
		result.GetStderr() <- []byte("INFO: log line\n")
		result.GetDone() <- true
	}()

	resp := collectRunCodeResponse(result)

	if resp.Stdout != "hello\n" {
		t.Fatalf("expected stdout to be preserved, got %q", resp.Stdout)
	}

	if resp.Stderr != "INFO: log line\n" {
		t.Fatalf("expected stderr to be preserved, got %q", resp.Stderr)
	}

	if resp.Error != "" {
		t.Fatalf("expected empty error on successful exit, got %q", resp.Error)
	}

	if resp.ExitCode != 0 {
		t.Fatalf("expected zero exit code, got %d", resp.ExitCode)
	}
}

func TestCollectRunCodeResponseIncludesExitCodeAndFullStderrOnFailure(t *testing.T) {
	result := newFakeOutputCaptureResult()
	result.exitCode = 7
	stderr := "Traceback (most recent call last):\nValueError: bad input\n"

	go func() {
		result.GetStdout() <- []byte("partial\n")
		result.GetStderr() <- []byte(stderr)
		result.GetDone() <- true
	}()

	resp := collectRunCodeResponse(result)

	if resp.ExitCode != 7 {
		t.Fatalf("expected exit code 7, got %d", resp.ExitCode)
	}

	if resp.Stderr != stderr {
		t.Fatalf("expected full stderr to be preserved, got %q", resp.Stderr)
	}

	if resp.Stdout != "partial\n" {
		t.Fatalf("expected stdout to remain raw on failure, got %q", resp.Stdout)
	}

	if !strings.Contains(resp.Error, "process exited with code 7") {
		t.Fatalf("expected error to include exit code, got %q", resp.Error)
	}

	if !strings.Contains(resp.Error, stderr) {
		t.Fatalf("expected error to include full stderr, got %q", resp.Error)
	}
}

func TestCollectRunCodeResponseIncludesExecutionFailureDetails(t *testing.T) {
	result := newFakeOutputCaptureResult()
	result.exitCode = -1

	go func() {
		result.GetExecError() <- []byte("error: timeout\n")
		result.GetDone() <- true
	}()

	resp := collectRunCodeResponse(result)

	if resp.Error != "process exited with code -1\nerror: timeout\n" {
		t.Fatalf("unexpected execution error: %q", resp.Error)
	}

	if resp.Stderr != "" {
		t.Fatalf("expected empty stderr, got %q", resp.Stderr)
	}

	if resp.ExitCode != -1 {
		t.Fatalf("expected timeout exit code -1, got %d", resp.ExitCode)
	}
}

func TestSuccessResponseJSONIncludesStderrAndExitCodeOnSuccess(t *testing.T) {
	resp := types.SuccessResponse(&RunCodeResponse{
		Stdout:   "<<RESULT>>ok<<RESULT>>\n",
		Stderr:   "INFO:root:Starting task...\n",
		Error:    "",
		ExitCode: 0,
	})

	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	var payload struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    RunCodeResponse `json:"data"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload.Code != 0 || payload.Message != "success" {
		t.Fatalf("expected success envelope, got code=%d message=%q", payload.Code, payload.Message)
	}

	if payload.Data.Stdout != "<<RESULT>>ok<<RESULT>>\n" {
		t.Fatalf("expected stdout field in JSON, got %q", payload.Data.Stdout)
	}

	if payload.Data.Stderr != "INFO:root:Starting task...\n" {
		t.Fatalf("expected stderr field in JSON, got %q", payload.Data.Stderr)
	}

	if payload.Data.Error != "" {
		t.Fatalf("expected empty error field in JSON, got %q", payload.Data.Error)
	}

	if payload.Data.ExitCode != 0 {
		t.Fatalf("expected exit_code field in JSON, got %d", payload.Data.ExitCode)
	}
}

func TestSuccessResponseJSONKeepsOuterSuccessForExecutionFailure(t *testing.T) {
	resp := types.SuccessResponse(&RunCodeResponse{
		Stdout:   "partial output\n",
		Stderr:   "Traceback (most recent call last):\nValueError: bad input\n",
		Error:    "process exited with code 1\nTraceback (most recent call last):\nValueError: bad input\n",
		ExitCode: 1,
	})

	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	var payload struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    RunCodeResponse `json:"data"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload.Code != 0 || payload.Message != "success" {
		t.Fatalf("expected outer success envelope for execution failure, got code=%d message=%q", payload.Code, payload.Message)
	}

	if payload.Data.Stdout != "partial output\n" {
		t.Fatalf("expected stdout to remain serialized on failure, got %q", payload.Data.Stdout)
	}

	if payload.Data.ExitCode != 1 {
		t.Fatalf("expected exit_code=1, got %d", payload.Data.ExitCode)
	}

	if !strings.Contains(payload.Data.Error, "process exited with code 1") {
		t.Fatalf("expected serialized error to include exit code, got %q", payload.Data.Error)
	}

	if !strings.Contains(payload.Data.Error, payload.Data.Stderr) {
		t.Fatalf("expected serialized error to include full stderr, got %q", payload.Data.Error)
	}
}
