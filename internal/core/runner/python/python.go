package python

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/langgenius/dify-sandbox/internal/core/runner"
	"github.com/langgenius/dify-sandbox/internal/static"
	"github.com/langgenius/dify-sandbox/internal/utils/log"
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

	stdout := make(chan []byte)
	stderr := make(chan []byte)
	done_chan := make(chan bool)

	err = p.WithTempDir([]string{
		temp_code_path,
		"/tmp/sandbox-python/python.so",
	}, func(root_path string) error {
		// create a new process
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		cmd := exec.CommandContext(ctx,
			"/usr/bin/python3",
			"-c",
			string(python_sandbox_fs),
			temp_code_path,
			strconv.Itoa(static.SANDBOX_USER_UID),
			strconv.Itoa(static.SANDBOX_GROUP_ID),
		)
		cmd.Env = []string{}

		// create a pipe for the stdout
		stdout_reader, err := cmd.StdoutPipe()
		if err != nil {
			cancel()
			return err
		}

		// create a pipe for the stderr
		stderr_reader, err := cmd.StderrPipe()
		if err != nil {
			cancel()
			return err
		}

		// start the process
		err = cmd.Start()
		if err != nil {
			cancel()
			return err
		}

		wg := sync.WaitGroup{}
		wg.Add(2)

		// read the output
		go func() {
			buf := make([]byte, 1024)
			defer wg.Done()
			for {
				n, err := stdout_reader.Read(buf)
				// exit if EOF
				if err != nil {
					if err == io.EOF {
						break
					} else {
						stderr <- []byte(fmt.Sprintf("error: %v\n", err))
						break
					}
				}
				stdout <- buf[:n]
			}
		}()

		// read the error
		go func() {
			buf := make([]byte, 1024)
			defer wg.Done()
			for {
				n, err := stderr_reader.Read(buf)
				// exit if EOF
				if err != nil {
					if err == io.EOF {
						break
					} else {
						stderr <- []byte(fmt.Sprintf("error: %v\n", err))
						break
					}
				}
				stderr <- buf[:n]
			}
		}()

		// wait for the process to finish
		go func() {
			status, err := cmd.Process.Wait()
			if err != nil {
				log.Error("process finished with status: %v", status.String())
				stderr <- []byte(fmt.Sprintf("error: %v\n", err))
			} else if status.ExitCode() != 0 {
				exit_string := status.String()
				if strings.Contains(exit_string, "bad system call (core dumped)") {
					stderr <- []byte("error: operation not permitted\n")
				} else {
					stderr <- []byte(fmt.Sprintf("exit code: %v\n", status.ExitCode()))
				}
			}

			// wait for the stdout and stderr to finish
			wg.Wait()
			stderr_reader.Close()
			stdout_reader.Close()
			os.Remove(temp_code_path)
			os.RemoveAll(root_path)
			os.Remove(root_path)
			cancel()
			done_chan <- true
		}()

		return nil
	})

	if err != nil {
		return nil, nil, nil, err
	}

	return stdout, stderr, done_chan, nil
}
