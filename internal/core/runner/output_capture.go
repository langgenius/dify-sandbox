package runner

import (
	"context"
	"fmt"
	"github.com/langgenius/dify-sandbox/internal/core/runner/isolation"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// LegacyOutputBytes is a finite ordinary-endpoint transport ceiling, not a
// Builder qualification budget. Builder streams use their immutable budgets.
const LegacyOutputBytes = 1 << 20

type OutputCaptureRunner struct {
	stdout, stderr  chan []byte
	done            chan bool
	timeout         time.Duration
	after_exit_hook func()
	cleanupHook     func() error
}

func NewOutputCaptureRunner() *OutputCaptureRunner {
	return &OutputCaptureRunner{stdout: make(chan []byte, 1), stderr: make(chan []byte, 1), done: make(chan bool, 1)}
}
func (s *OutputCaptureRunner) WriteError(data []byte) {
	select {
	case s.stderr <- append([]byte(nil), data...):
	default:
	}
}
func (s *OutputCaptureRunner) WriteOutput(data []byte) {
	select {
	case s.stdout <- append([]byte(nil), data...):
	default:
	}
}
func (s *OutputCaptureRunner) SetAfterExitHook(hook func())     { s.after_exit_hook = hook }
func (s *OutputCaptureRunner) SetCleanupHook(hook func() error) { s.cleanupHook = hook }
func (s *OutputCaptureRunner) SetTimeout(timeout time.Duration) { s.timeout = timeout }
func (s *OutputCaptureRunner) CaptureOutput(ctx context.Context, cmd *exec.Cmd) error {
	timeout := s.timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	stdout, wout, err := os.Pipe()
	if err != nil {
		cancel()
		return err
	}
	stderr, werr, err := os.Pipe()
	if err != nil {
		stdout.Close()
		wout.Close()
		cancel()
		return err
	}
	cmd.Stdout = wout
	cmd.Stderr = werr
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	if err = cmd.Start(); err != nil {
		stdout.Close()
		stderr.Close()
		wout.Close()
		werr.Close()
		cancel()
		return err
	}
	wout.Close()
	werr.Close()
	go func() {
		defer cancel()
		// Keep the group leader unreaped until group termination is finished, so its
		// numeric PID cannot be reused while the cleanup signals are in flight.
		exited := make(chan error, 1)
		go func() {
			var info unix.Siginfo
			for {
				err := unix.Waitid(unix.P_PID, cmd.Process.Pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
				if err == unix.EINTR {
					continue
				}
				exited <- err
				return
			}
		}()
		captureDone := make(chan isolation.CaptureResult, 1)
		go func() {
			captureDone <- isolation.Capture(runCtx, stdout, stderr, isolation.Budget{StdoutBytes: LegacyOutputBytes, StderrBytes: LegacyOutputBytes})
		}()
		leaderExited := false
		var result isolation.CaptureResult
		select {
		case result = <-captureDone:
		case <-exited:
			leaderExited = true
			// A child retaining stdout gets only a finite drain grace.
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case result = <-captureDone:
			case <-timer.C:
				cancel()
				result = <-captureDone
				result.Failure = "output_read"
			}
			timer.Stop()
		}
		if result.Failure == "" && !leaderExited {
			select {
			case <-exited:
				leaderExited = true
			case <-runCtx.Done():
				result.Failure = "cancelled"
				if runCtx.Err() == context.DeadlineExceeded {
					result.Failure = "deadline"
				}
			}
		}
		unix.Kill(-cmd.Process.Pid, unix.SIGKILL)
		if !leaderExited {
			select {
			case <-exited:
			case <-time.After(100 * time.Millisecond):
				go func() { <-exited; cmd.Wait() }()
				s.stderr <- []byte("error: cleanup_failed\n")
				s.done <- false
				return
			}
		}
		waitErr := cmd.Wait()
		success := result.Failure == ""
		if result.Failure != "" {
			result.Stderr = []byte("error: " + result.Failure + "\n")
		} else if waitErr != nil {
			result.Stderr = append(result.Stderr, []byte(fmt.Sprintf("error: %v\n", waitErr))...)
			if len(result.Stderr) > LegacyOutputBytes {
				result.Stderr = []byte("error: stderr_limit\n")
				success = false
			}
		}
		if s.cleanupHook != nil {
			if err := s.cleanupHook(); err != nil {
				success = false
				result.Stderr = []byte("error: cleanup_failed\n")
			}
		}
		if s.after_exit_hook != nil {
			s.after_exit_hook()
		}
		s.stdout <- result.Stdout
		s.stderr <- result.Stderr
		s.done <- success
	}()
	return nil
}
func (s *OutputCaptureRunner) GetStdout() chan []byte { return s.stdout }
func (s *OutputCaptureRunner) GetStderr() chan []byte { return s.stderr }
func (s *OutputCaptureRunner) GetDone() chan bool     { return s.done }
