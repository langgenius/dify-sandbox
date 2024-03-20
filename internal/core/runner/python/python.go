package python

import (
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

func init() {
	log.Info("initializing python runner environment...")
	// remove /tmp/sandbox-python
	os.RemoveAll("/tmp/sandbox-python")
	os.Remove("/tmp/sandbox-python")

	err := os.MkdirAll("/tmp/sandbox-python", 0755)
	if err != nil {
		log.Panic("failed to create /tmp/sandbox-python")
	}
	err = os.WriteFile("/tmp/sandbox-python/python.so", python_lib, 0755)
	if err != nil {
		log.Panic("failed to write /tmp/sandbox-python/python.so")
	}
	log.Info("python runner environment initialized")
}

func (p *PythonRunner) Run(code string, timeout time.Duration, stdin []byte) (chan []byte, chan []byte, chan bool, error) {
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

	stdout := make(chan []byte, 42)
	stderr := make(chan []byte, 42)

	write_out := func(data []byte) {
		stdout <- data
	}

	write_err := func(data []byte) {
		stderr <- data
	}

	done_chan := make(chan bool)

	err = p.WithTempDir([]string{
		temp_code_path,
		"/tmp/sandbox-python/python.so",
	}, func(root_path string) error {
		// create a new process
		cmd := exec.Command(
			static.GetDifySandboxGlobalConfigurations().PythonPath,
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
			return err
		}

		// create a pipe for the stderr
		stderr_reader, err := cmd.StderrPipe()
		if err != nil {
			stdout_reader.Close()
			return err
		}

		// start the process
		err = cmd.Start()
		if err != nil {
			stdout_reader.Close()
			stderr_reader.Close()
			return err
		}

		// start a timer for the timeout
		timer := time.NewTimer(timeout)
		go func() {
			<-timer.C
			if cmd != nil && cmd.Process != nil {
				// write the error
				write_err([]byte("error: timeout\n"))
				// send a signal to the process
				cmd.Process.Kill()
			}
		}()

		wg := sync.WaitGroup{}
		wg.Add(2)

		// read the output
		go func() {
			defer wg.Done()
			for {
				buf := make([]byte, 1024)
				n, err := stdout_reader.Read(buf)
				// exit if EOF
				if err != nil {
					if err == io.EOF {
						break
					} else {
						write_err([]byte(fmt.Sprintf("error: %v\n", err)))
						break
					}
				}
				write_out(buf[:n])
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
						write_err([]byte(fmt.Sprintf("error: %v\n", err)))
						break
					}
				}
				write_err(buf[:n])
			}
		}()

		// wait for the process to finish
		go func() {
			status, err := cmd.Process.Wait()
			if err != nil {
				log.Error("process finished with status: %v", status.String())
				write_err([]byte(fmt.Sprintf("error: %v\n", err)))
			} else if status.ExitCode() != 0 {
				exit_string := status.String()
				if strings.Contains(exit_string, "bad system call (core dumped)") {
					write_err([]byte("error: bad system call\n"))
				} else {
					write_err([]byte(fmt.Sprintf("error: %v\n", status.String())))
				}
			}

			// wait for the stdout and stderr to finish
			wg.Wait()
			stderr_reader.Close()
			stdout_reader.Close()
			os.Remove(temp_code_path)
			os.RemoveAll(root_path)
			os.Remove(root_path)
			timer.Stop()
			done_chan <- true
		}()

		return nil
	})

	if err != nil {
		return nil, nil, nil, err
	}

	return stdout, stderr, done_chan, nil
}
