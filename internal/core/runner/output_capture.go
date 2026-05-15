package runner

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// OutputCaptureResult keeps raw process stderr separate from sandbox-generated
// execution failures. stderr carries the user/runtime stderr stream exactly as
// emitted by the process, while execError is reserved for sandbox/runtime
// failure messages such as timeout, pipe read failure, or wait failure.
type OutputCaptureResult struct {
	stdout    chan []byte
	stderr    chan []byte
	execError chan []byte
	done      chan bool

	exitCodeMu sync.RWMutex
	exitCode   int
}

func NewOutputCaptureResult() *OutputCaptureResult {
	return &OutputCaptureResult{
		stdout:    make(chan []byte),
		stderr:    make(chan []byte),
		execError: make(chan []byte),
		done:      make(chan bool),
	}
}

func (r *OutputCaptureResult) GetStdout() chan []byte {
	return r.stdout
}

func (r *OutputCaptureResult) GetStderr() chan []byte {
	return r.stderr
}

func (r *OutputCaptureResult) GetExecError() chan []byte {
	return r.execError
}

func (r *OutputCaptureResult) GetDone() chan bool {
	return r.done
}

func (r *OutputCaptureResult) SetExitCode(code int) {
	r.exitCodeMu.Lock()
	defer r.exitCodeMu.Unlock()
	r.exitCode = code
}

func (r *OutputCaptureResult) GetExitCode() int {
	r.exitCodeMu.RLock()
	defer r.exitCodeMu.RUnlock()
	return r.exitCode
}

type OutputCaptureRunner struct {
	result *OutputCaptureResult

	timeout time.Duration

	after_exit_hook func()
}

func NewOutputCaptureRunner() *OutputCaptureRunner {
	return &OutputCaptureRunner{
		result: NewOutputCaptureResult(),
	}
}

func (s *OutputCaptureRunner) WriteExecError(data []byte) {
	if s.result != nil && s.result.execError != nil {
		s.result.execError <- data
	}
}

func (s *OutputCaptureRunner) WriteStderr(data []byte) {
	if s.result != nil && s.result.stderr != nil {
		s.result.stderr <- data
	}
}

func (s *OutputCaptureRunner) WriteOutput(data []byte) {
	if s.result != nil && s.result.stdout != nil {
		s.result.stdout <- data
	}
}

func (s *OutputCaptureRunner) SetAfterExitHook(hook func()) {
	s.after_exit_hook = hook
}

func (s *OutputCaptureRunner) SetTimeout(timeout time.Duration) {
	s.timeout = timeout
}

func (s *OutputCaptureRunner) CaptureOutput(ctx context.Context, cmd *exec.Cmd) error {
	// start a timer for the timeout
	timeout := s.timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	timer := time.AfterFunc(timeout, func() {
		if cmd != nil && cmd.Process != nil {
			s.result.SetExitCode(-1)
			s.WriteExecError([]byte("error: timeout\n"))
			// send a signal to the process
			cmd.Process.Kill()
		}
	})

	// create a pipe for the stdout
	stdoutReader, err := cmd.StdoutPipe()
	if err != nil {
		timer.Stop()
		return err
	}

	// create a pipe for the stderr
	stderrReader, err := cmd.StderrPipe()
	if err != nil {
		stdoutReader.Close()
		timer.Stop()
		return err
	}

	// start the process
	err = cmd.Start()
	if err != nil {
		stdoutReader.Close()
		stderrReader.Close()
		timer.Stop()
		return err
	}

	wg := sync.WaitGroup{}
	wg.Add(2)

	// read the output
	go func() {
		defer wg.Done()
		defer stdoutReader.Close()
		for {
			buf := make([]byte, 1024)
			n, err := stdoutReader.Read(buf)
			// exit if EOF
			if err != nil {
				if err == io.EOF {
					break
				} else {
					s.WriteExecError([]byte(fmt.Sprintf("error: %v\n", err)))
					break
				}
			}
			s.WriteOutput(buf[:n])
		}
	}()

	// read the error
	go func() {
		buf := make([]byte, 1024)
		defer wg.Done()
		defer stderrReader.Close()
		for {
			n, err := stderrReader.Read(buf)
			// exit if EOF
			if err != nil {
				if err == io.EOF {
					break
				} else {
					s.WriteExecError([]byte(fmt.Sprintf("error: %v\n", err)))
					break
				}
			}
			s.WriteStderr(buf[:n])
		}
	}()

	// wait for the process to finish
	go func() {
		defer timer.Stop()

		// wait for the stdout and stderr to finish
		wg.Wait()

		// wait for the process to finish
		status, err := cmd.Process.Wait()
		if err != nil {
			statusText := ""
			if status != nil {
				statusText = status.String()
			}
			slog.ErrorContext(ctx, "process finished with error", "status", statusText, "err", err)
			if s.result.GetExitCode() == 0 {
				s.result.SetExitCode(-1)
			}
			s.WriteExecError([]byte(fmt.Sprintf("error: %v\n", err)))
		} else if status != nil {
			s.result.SetExitCode(status.ExitCode())
			if status.ExitCode() != 0 {
				exitString := status.String()
				slog.ErrorContext(ctx, "process finished with error", "status", exitString)
				if strings.Contains(exitString, "bad system call") {
					s.WriteExecError([]byte("error: operation not permitted\n"))
				}
			}
		}

		if s.after_exit_hook != nil {
			s.after_exit_hook()
		}

		s.result.done <- true
	}()

	return nil
}

func (s *OutputCaptureRunner) Result() *OutputCaptureResult {
	return s.result
}
