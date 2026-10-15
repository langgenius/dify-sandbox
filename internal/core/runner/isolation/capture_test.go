package isolation

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestOutputLimitIsFailure(t *testing.T) {
	b := testProfile(t).Limits
	b.StdoutBytes = 4
	r := Capture(context.Background(), strings.NewReader("abcdef"), strings.NewReader(""), b)
	if r.Failure != "stdout_limit" || len(r.Stdout) > 4 {
		t.Fatalf("unbounded or successful capture: %#v", r)
	}
}
func TestCaptureStderrUTF8AndCancellation(t *testing.T) {
	b := testProfile(t).Limits
	b.StdoutBytes = 4
	b.StderrBytes = 4
	for _, tc := range []struct{ out, err, reason string }{{"", "abcdef", "stderr_limit"}, {"€€", "", "stdout_limit"}, {"\xff", "", "invalid_output"}, {"€", "", ""}} {
		r := Capture(context.Background(), strings.NewReader(tc.out), strings.NewReader(tc.err), b)
		if r.Failure != tc.reason || len(r.Stdout) > 4 || len(r.Stderr) > 4 {
			t.Fatalf("%+v", r)
		}
	}
	rd, wr := io.Pipe()
	defer wr.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	r := Capture(ctx, rd, strings.NewReader(""), b)
	if r.Failure != "deadline" || time.Since(started) > time.Second {
		t.Fatalf("blocked capture: %+v", r)
	}
}
