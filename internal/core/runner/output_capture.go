package runner

import (
	"context"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

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
		captureDone := make(chan ordinaryCaptureResult, 1)
		go func() {
			captureDone <- captureOrdinary(runCtx, stdout, stderr)
		}()
		leaderExited := false
		var result ordinaryCaptureResult
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
			reason := result.Failure
			if reason == "deadline" {
				reason = "timeout"
			}
			result.Stderr = append(result.Stderr, []byte("error: "+reason+"\n")...)
		}
		if waitErr != nil {
			message := fmt.Sprintf("error: %v\n", waitErr)
			if strings.Contains(waitErr.Error(), "bad system call") {
				message = "error: operation not permitted\n"
			}
			result.Stderr = append(result.Stderr, []byte(message)...)
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

// Ordinary endpoints historically accept arbitrary output bytes without a byte
// budget. Builder's bounded, UTF-8-validating capture is intentionally separate.
type ordinaryCaptureResult struct {
	Stdout, Stderr []byte
	Failure        string
}

func captureOrdinary(ctx context.Context, stdout, stderr *os.File) ordinaryCaptureResult {
	type stream struct {
		data []byte
		err  error
	}
	out, errout := make(chan stream, 1), make(chan stream, 1)
	read := func(f *os.File, done chan<- stream) { defer f.Close(); b, err := io.ReadAll(f); done <- stream{b, err} }
	go read(stdout, out)
	go read(stderr, errout)
	var a, b stream
	var failure string
	for out != nil || errout != nil {
		select {
		case a = <-out:
			out = nil
		case b = <-errout:
			errout = nil
		case <-ctx.Done():
			stdout.Close()
			stderr.Close()
			if out != nil {
				a = <-out
				out = nil
			}
			if errout != nil {
				b = <-errout
				errout = nil
			}
			failure = "cancelled"
			if ctx.Err() == context.DeadlineExceeded {
				failure = "deadline"
			}
		}
	}
	if failure == "" && (a.err != nil || b.err != nil) {
		failure = "output_read"
	}
	return ordinaryCaptureResult{Stdout: a.data, Stderr: b.data, Failure: failure}
}
