package runner

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type OutputCaptureRunner struct {
	stdout chan []byte
	stderr chan []byte
	done   chan bool

	timeout time.Duration

	afterExitHook func()

	// closeOnce ensures we only signal done once
	closeOnce sync.Once
	// closed is set when the done signal has been sent;
	// after that, no more writes to stdout/stderr are safe
	// because the consumer closes those channels upon receiving done.
	closed chan struct{}
}

func NewOutputCaptureRunner() *OutputCaptureRunner {
	return &OutputCaptureRunner{
		stdout: make(chan []byte),
		stderr: make(chan []byte),
		done:   make(chan bool),
		closed: make(chan struct{}),
	}
}

// sendOut sends data to stdout, unless the runner is already done.
func (s *OutputCaptureRunner) sendOut(data []byte) {
	select {
	case <-s.closed:
		return
	case s.stdout <- data:
	}
}

// sendErr sends data to stderr, unless the runner is already done.
func (s *OutputCaptureRunner) sendErr(data []byte) {
	select {
	case <-s.closed:
		return
	case s.stderr <- data:
	}
}

// signalDone sends the done signal exactly once and marks the runner as closed.
func (s *OutputCaptureRunner) signalDone() {
	s.closeOnce.Do(func() {
		close(s.closed)
		s.done <- true
	})
}

func (s *OutputCaptureRunner) SetAfterExitHook(hook func()) {
	s.afterExitHook = hook
}

func (s *OutputCaptureRunner) SetTimeout(timeout time.Duration) {
	s.timeout = timeout
}

func (s *OutputCaptureRunner) CaptureOutput(ctx context.Context, cmd *exec.Cmd) error {
	timeout := s.timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	stdoutReader, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	stderrReader, err := cmd.StderrPipe()
	if err != nil {
		stdoutReader.Close()
		return err
	}

	if err := cmd.Start(); err != nil {
		stdoutReader.Close()
		stderrReader.Close()
		return err
	}

	// Kill the process on timeout. We use timedOut to detect
	// whether the kill was caused by the timer, since timer.Stop()
	// alone is racy with AfterFunc's goroutine.
	var timedOut atomic.Bool
	timer := time.AfterFunc(timeout, func() {
		timedOut.Store(true)
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	})

	var wg sync.WaitGroup
	wg.Add(2)

	readPipe := func(reader io.ReadCloser, sender func([]byte)) {
		defer wg.Done()
		defer reader.Close()
		for {
			buf := make([]byte, 1024)
			n, err := reader.Read(buf)
			if n > 0 {
				sender(buf[:n])
			}
			if err != nil {
				if err != io.EOF {
					s.sendErr([]byte(fmt.Sprintf("error: %v\n", err)))
				}
				return
			}
		}
	}

	go readPipe(stdoutReader, s.sendOut)
	go readPipe(stderrReader, s.sendErr)

	go func() {
		defer timer.Stop()

		wg.Wait()
		err := cmd.Wait()

		if err != nil {
			errMsg := err.Error()
			slog.ErrorContext(ctx, "process finished with error", "err", err)
			if timedOut.Load() {
				s.sendErr([]byte("error: timeout\n"))
			} else if strings.Contains(errMsg, "bad system call") {
				s.sendErr([]byte("error: operation not permitted\n"))
			} else {
				s.sendErr([]byte(fmt.Sprintf("error: %v\n", errMsg)))
			}
		}

		if s.afterExitHook != nil {
			s.afterExitHook()
		}
		s.signalDone()
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
