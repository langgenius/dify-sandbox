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

type OutputCaptureRunner struct {
	stdout chan []byte
	stderr chan []byte
	done   chan bool

	timeout time.Duration

	after_exit_hook func()
}

func NewOutputCaptureRunner() *OutputCaptureRunner {
	return &OutputCaptureRunner{
		stdout: make(chan []byte),
		stderr: make(chan []byte),
		done:   make(chan bool),
	}
}

func (s *OutputCaptureRunner) WriteError(data []byte) {
	if s.stderr != nil {
		s.stderr <- data
	}
}

func (s *OutputCaptureRunner) WriteOutput(data []byte) {
	if s.stdout != nil {
		s.stdout <- data
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
			// write the error
			s.WriteError([]byte("error: timeout\n"))
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

	written := 0

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
					s.WriteError([]byte(fmt.Sprintf("error: %v\n", err)))
					break
				}
			}
			written += n
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
					s.WriteError([]byte(fmt.Sprintf("error: %v\n", err)))
					break
				}
			}
			s.WriteError(buf[:n])
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
			slog.ErrorContext(ctx, "process finished with error", "status", status.String(), "err", err)
			s.WriteError([]byte(fmt.Sprintf("error: %v\n", err)))
		} else if status.ExitCode() != 0 {
			exitString := status.String()
			slog.ErrorContext(ctx, "process finished with error", "status", status.String())
			if strings.Contains(exitString, "bad system call") {
				s.WriteError([]byte("error: operation not permitted\n"))
			} else {
				s.WriteError([]byte(fmt.Sprintf("error: %v\n", exitString)))
			}
		}

		if s.after_exit_hook != nil {
			s.after_exit_hook()
		}

		s.done <- true
	}()

	return nil
}

func (s *OutputCaptureRunner) GetStdout() chan []byte {
	return s.stdout
}

func (s *OutputCaptureRunner) GetStderr() chan []byte {
	return s.stderr
}

func (s *OutputCaptureRunner) GetDone() chan bool {
	return s.done
}
