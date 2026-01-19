package runner

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/langgenius/dify-sandbox/internal/utils/log"
)

const (
	errorFormat = "error: %v\n"
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

func (s *OutputCaptureRunner) CaptureOutput(cmd *exec.Cmd) error {
	timeout := s.getTimeout()
	timer := s.setupTimeoutTimer(cmd, timeout)

	stdoutReader, stderrReader, err := s.setupPipes(cmd)
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		s.closePipes(stdoutReader, stderrReader)
		return err
	}

	s.startOutputReaders(stdoutReader, stderrReader, timer, cmd)

	return nil
}

func (s *OutputCaptureRunner) getTimeout() time.Duration {
	if s.timeout == 0 {
		return 5 * time.Second
	}
	return s.timeout
}

func (s *OutputCaptureRunner) setupTimeoutTimer(cmd *exec.Cmd, timeout time.Duration) *time.Timer {
	return time.AfterFunc(timeout, func() {
		if cmd != nil && cmd.Process != nil {
			s.WriteError([]byte("error: timeout\n"))
			cmd.Process.Kill()
		}
	})
}

func (s *OutputCaptureRunner) setupPipes(cmd *exec.Cmd) (io.ReadCloser, io.ReadCloser, error) {
	stdoutReader, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}

	stderrReader, err := cmd.StderrPipe()
	if err != nil {
		stdoutReader.Close()
		return nil, nil, err
	}

	return stdoutReader, stderrReader, nil
}

func (s *OutputCaptureRunner) closePipes(stdoutReader, stderrReader io.ReadCloser) {
	if stdoutReader != nil {
		stdoutReader.Close()
	}
	if stderrReader != nil {
		stderrReader.Close()
	}
}

func (s *OutputCaptureRunner) startOutputReaders(stdoutReader, stderrReader io.ReadCloser, timer *time.Timer, cmd *exec.Cmd) {
	wg := sync.WaitGroup{}
	wg.Add(2)

	go s.readStdout(stdoutReader, &wg)
	go s.readStderr(stderrReader, &wg)
	go s.waitForProcess(&wg, timer, cmd)
}

func (s *OutputCaptureRunner) readStdout(reader io.ReadCloser, wg *sync.WaitGroup) {
	defer wg.Done()
	s.readStream(reader, s.WriteOutput)
}

func (s *OutputCaptureRunner) readStderr(reader io.ReadCloser, wg *sync.WaitGroup) {
	defer wg.Done()
	s.readStream(reader, s.WriteError)
}

func (s *OutputCaptureRunner) readStream(reader io.ReadCloser, writer func([]byte)) {
	buf := make([]byte, 1024)
	for {
		n, err := reader.Read(buf)
		if err != nil {
			if err != io.EOF {
				s.WriteError([]byte(fmt.Sprintf(errorFormat, err)))
			}
			break
		}
		writer(buf[:n])
	}
}

func (s *OutputCaptureRunner) waitForProcess(wg *sync.WaitGroup, timer *time.Timer, cmd *exec.Cmd) {
	wg.Wait()
	s.handleProcessStatus(cmd)
	s.executeAfterExitHook()
	timer.Stop()
	s.done <- true
}

func (s *OutputCaptureRunner) handleProcessStatus(cmd *exec.Cmd) {
	status, err := cmd.Process.Wait()
	if err != nil {
		log.Error("process finished with status: %v", status.String())
		s.WriteError([]byte(fmt.Sprintf(errorFormat, err)))
		return
	}

	if status.ExitCode() != 0 {
		s.handleNonZeroExit(status)
	}
}

func (s *OutputCaptureRunner) handleNonZeroExit(status *os.ProcessState) {
	exitString := status.String()
	if strings.Contains(exitString, "bad system call") {
		s.WriteError([]byte("error: operation not permitted\n"))
	} else {
		s.WriteError([]byte(fmt.Sprintf(errorFormat, exitString)))
	}
}

func (s *OutputCaptureRunner) executeAfterExitHook() {
	if s.after_exit_hook != nil {
		s.after_exit_hook()
	}
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
