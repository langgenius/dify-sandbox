package service

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner"
)

func TestOrdinaryLargeOutputPreservesEnvelope(t *testing.T) {
	r := runner.NewOutputCaptureRunner()
	r.SetTimeout(5 * time.Second)
	if err := r.CaptureOutput(context.Background(), exec.Command("/bin/sh", "-c", "head -c 1048577 /dev/zero; head -c 1048577 /dev/zero >&2")); err != nil {
		t.Fatal(err)
	}
	result := collectOutput(r.GetStdout(), r.GetStderr(), r.GetDone())
	if result.Code != 0 {
		t.Fatalf("ordinary large output rejected: code=%d", result.Code)
	}
	data, ok := result.Data.(*RunCodeResponse)
	if !ok {
		t.Fatalf("missing ordinary stdout/error envelope: %T", result.Data)
	}
	if data.Stdout != strings.Repeat("\x00", 1048577) || data.Stderr != strings.Repeat("\x00", 1048577) {
		t.Fatalf("truncated streams: stdout=%d stderr=%d", len(data.Stdout), len(data.Stderr))
	}
	encoded, err := json.Marshal(result)
	if err != nil || !strings.Contains(string(encoded), `"stdout":`) || !strings.Contains(string(encoded), `"error":`) {
		t.Fatal("ordinary JSON shape lost", err)
	}
}

func TestOrdinarySignalAndTimeoutEnvelope(t *testing.T) {
	for _, tc := range []struct {
		name, script, want string
		timeout            time.Duration
	}{
		{"seccomp", "kill -SYS $$", "error: operation not permitted\n", time.Second},
		{"exit", "printf before; printf diagnostic >&2; exit 7", "diagnosticerror: exit status 7\n", time.Second},
		{"timeout", "printf before; sleep 30", "error: timeout\nerror: signal: killed\n", 50 * time.Millisecond},
		{"raw bytes", "printf '\\377'", "", time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := runner.NewOutputCaptureRunner()
			r.SetTimeout(tc.timeout)
			if err := r.CaptureOutput(context.Background(), exec.Command("/bin/sh", "-c", tc.script)); err != nil {
				t.Fatal(err)
			}
			result := collectOutput(r.GetStdout(), r.GetStderr(), r.GetDone())
			if result.Code != 0 {
				t.Fatalf("lost ordinary completion envelope: %#v", result)
			}
			data, ok := result.Data.(*RunCodeResponse)
			if !ok {
				t.Fatalf("missing envelope %T", result.Data)
			}
			if data.Stderr != tc.want {
				t.Fatalf("stderr=%q want=%q", data.Stderr, tc.want)
			}
			if tc.name == "raw bytes" && data.Stdout != "\xff" {
				t.Fatalf("raw bytes rejected: %q", data.Stdout)
			}
		})
	}
}
