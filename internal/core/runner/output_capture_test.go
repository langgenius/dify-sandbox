package runner

import (
	"context"
	"fmt"
	"os/exec"
	"testing"
	"time"
)

func TestCollectorDeadlineAndBlockedConsumer(t *testing.T) {
	r := NewOutputCaptureRunner()
	r.SetTimeout(100 * time.Millisecond)
	exited := make(chan struct{})
	r.SetAfterExitHook(func() { close(exited) })
	if err := r.CaptureOutput(context.Background(), exec.Command("/bin/sh", "-c", "printf output; sleep 30")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	case <-time.After(3 * time.Second):
		t.Fatal("blocked output sink prevented kill/cleanup")
	}
	select {
	case success := <-r.GetDone():
		if success {
			t.Fatal("deadline reported success")
		}
	case <-time.After(time.Second):
		t.Fatal("no failure result")
	}
}
func TestCollectorPreservesFinalBytesAndReapsDescendantPipe(t *testing.T) {
	for _, script := range []string{"printf final", "sleep 30 & printf final"} {
		r := NewOutputCaptureRunner()
		r.SetTimeout(100 * time.Millisecond)
		if err := r.CaptureOutput(context.Background(), exec.Command("/bin/sh", "-c", script)); err != nil {
			t.Fatal(err)
		}
		var out string
		deadline := time.After(2 * time.Second)
	loop:
		for {
			select {
			case b := <-r.GetStdout():
				out += string(b)
			case <-r.GetStderr():
			case <-r.GetDone():
				select {
				case b := <-r.GetStdout():
					out += string(b)
				default:
				}
				break loop
			case <-deadline:
				t.Fatal("descendant held pipe forever")
			}
		}
		if out != "final" {
			t.Fatalf("lost final bytes: %q", out)
		}
	}
}
func TestCollectorCleanupFailureIsNotSuccess(t *testing.T) {
	r := NewOutputCaptureRunner()
	r.SetCleanupHook(func() error { return fmt.Errorf("forced cleanup failure") })
	if err := r.CaptureOutput(context.Background(), exec.Command("/bin/sh", "-c", "printf result")); err != nil {
		t.Fatal(err)
	}
	if <-r.GetDone() {
		t.Fatal("cleanup failure reported success")
	}
}
