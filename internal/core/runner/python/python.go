package python

import (
	_ "embed"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/langgenius/dify-sandbox/internal/core/runner"
	"github.com/langgenius/dify-sandbox/internal/static"
)

type PythonRunner struct {
	runner.Runner
	runner.SeccompRunner
}

//go:embed prescript.py
var python_sandbox_fs []byte

//go:embed python.so
var python_lib []byte

func (p *PythonRunner) Run(code string, timeout time.Duration, stdin []byte) (chan []byte, chan []byte, chan bool, error) {
	// check if libpython.so exists
	if _, err := os.Stat("/tmp/sandbox-python/python.so"); os.IsNotExist(err) {
		err := os.MkdirAll("/tmp/sandbox-python", 0755)
		if err != nil {
			return nil, nil, nil, err
		}
		err = os.WriteFile("/tmp/sandbox-python/python.so", python_lib, 0755)
		if err != nil {
			return nil, nil, nil, err
		}
	}

	// create a tmp dir and copy the python script
	temp_code_name := strings.ReplaceAll(uuid.New().String(), "-", "_")
	temp_code_name = strings.ReplaceAll(temp_code_name, "/", ".")
	temp_code_path := fmt.Sprintf("/tmp/code/%s.py", temp_code_name)
	err := os.MkdirAll("/tmp/code", 0755)
	if err != nil {
		return nil, nil, nil, err
	}
	err = os.WriteFile(temp_code_path, []byte(code), 0755)
	if err != nil {
		return nil, nil, nil, err
	}

	stdout := make(chan []byte, 1)
	stderr := make(chan []byte, 1)
	done_chan := make(chan bool, 1)

	err = p.WithTempDir([]string{
		temp_code_path,
		"/tmp/sandbox-python/python.so",
	}, func(root_path string) error {
		var pipe_fds [2]int
		// create stdout pipe
		err = syscall.Pipe2(pipe_fds[0:], syscall.O_CLOEXEC)
		if err != nil {
			return err
		}
		stdout_reader, stdout_writer := pipe_fds[0], pipe_fds[1]
		// create stderr pipe
		err = syscall.Pipe2(pipe_fds[0:], syscall.O_CLOEXEC)
		if err != nil {
			return err
		}
		stderr_reader, stderr_writer := pipe_fds[0], pipe_fds[1]

		// create a new process
		pid, _, errno := syscall.RawSyscall(syscall.SYS_FORK, 0, 0, 0)
		if errno != 0 {
			return fmt.Errorf("failed to fork: %v", errno)
		}

		if pid == 0 {
			// child process
			syscall.Close(stdout_reader)
			syscall.Close(stderr_reader)

			// dup the stdout and stderr
			syscall.Dup2(stdout_writer, int(os.Stdout.Fd()))
			syscall.Dup2(stderr_writer, int(os.Stderr.Fd()))
			err := syscall.Exec(
				"/usr/bin/python3",
				[]string{
					"/usr/bin/python3",
					"-c",
					string(python_sandbox_fs),
					temp_code_path,
					strconv.Itoa(static.SANDBOX_USER_UID),
					strconv.Itoa(static.SANDBOX_GROUP_ID),
				},
				nil,
			)

			if err != nil {
				stderr <- []byte(fmt.Sprintf("failed to exec: %v", err))
				return nil
			}
		} else {
			syscall.Close(stdout_writer)
			syscall.Close(stderr_writer)

			// read the output
			go func() {
				buf := make([]byte, 1024)
				for {
					n, err := syscall.Read(stdout_reader, buf)
					if err != nil {
						break
					}
					stdout <- buf[:n]
				}
			}()

			// read the error
			go func() {
				buf := make([]byte, 1024)
				for {
					n, err := syscall.Read(stderr_reader, buf)
					if err != nil {
						break
					}
					stderr <- buf[:n]
				}
			}()

			// wait for the process to finish
			done := make(chan error, 1)
			go func() {
				var status syscall.WaitStatus
				_, err := syscall.Wait4(int(pid), &status, 0, nil)
				time.Sleep(time.Second)
				if err != nil {
					done <- err
					return
				}
				done <- nil
			}()

			go func() {
				for {
					select {
					case <-time.After(timeout):
						// kill the process
						syscall.Kill(int(pid), syscall.SIGKILL)
						stderr <- []byte("timeout\n")
					case err := <-done:
						if err != nil {
							stderr <- []byte(fmt.Sprintf("error: %v\n", err))
						}
						os.Remove(temp_code_path)
						os.RemoveAll(root_path)
						os.Remove(root_path)
						syscall.Close(stdout_reader)
						syscall.Close(stderr_reader)
						done_chan <- true
						return
					}
				}
			}()
		}

		return nil
	})

	if err != nil {
		return nil, nil, nil, err
	}

	return stdout, stderr, done_chan, nil
}
