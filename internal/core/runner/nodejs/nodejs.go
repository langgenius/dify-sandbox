package nodejs

import (
	"embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner"
	"github.com/langgenius/dify-sandbox/internal/static"
	"github.com/langgenius/dify-sandbox/internal/utils/log"
)

type NodeJsRunner struct {
	runner.Runner
	runner.SeccompRunner
}

//go:embed prescript.js
var nodejs_sandbox_fs []byte

//go:embed nodejs.so
var nodejs_lib []byte

//go:embed dependens
var nodejs_dependens embed.FS // it's a directory

func init() {
	log.Info("initializing nodejs runner environment...")
	os.RemoveAll("/tmp/sandbox-nodejs")
	os.Remove("/tmp/sandbox-nodejs")

	err := os.MkdirAll("/tmp/sandbox-nodejs", 0755)
	if err != nil {
		log.Panic("failed to create /tmp/sandbox-nodejs")
	}
	err = os.WriteFile("/tmp/sandbox-nodejs/nodejs.so", nodejs_lib, 0755)
	if err != nil {
		log.Panic("failed to write /tmp/sandbox-nodejs/nodejs.so")
	}

	// remove /tmp/sandbox-nodejs-project
	os.RemoveAll("/tmp/sandbox-nodejs-project")
	os.Remove("/tmp/sandbox-nodejs-project")
	// copy the nodejs project into /tmp/sandbox-nodejs-project
	err = os.MkdirAll("/tmp/sandbox-nodejs-project", 0755)
	if err != nil {
		log.Panic("failed to create /tmp/sandbox-nodejs-project")
	}

	// copy the nodejs project into /tmp/sandbox-nodejs-project
	var recursively_copy func(src string, dst string) error
	recursively_copy = func(src string, dst string) error {
		entries, err := nodejs_dependens.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			src_path := src + "/" + entry.Name()
			dst_path := dst + "/" + entry.Name()
			if entry.IsDir() {
				err = os.Mkdir(dst_path, 0755)
				if err != nil {
					return err
				}
				err = recursively_copy(src_path, dst_path)
				if err != nil {
					return err
				}
			} else {
				data, err := nodejs_dependens.ReadFile(src_path)
				if err != nil {
					return err
				}
				err = os.WriteFile(dst_path, data, 0755)
				if err != nil {
					return err
				}
			}
		}
		return nil
	}

	err = recursively_copy("dependens", "/tmp/sandbox-nodejs-project")
	if err != nil {
		log.Panic("failed to copy nodejs project")
	}
	log.Info("nodejs runner environment initialized")
}

func (p *NodeJsRunner) Run(code string, timeout time.Duration, stdin []byte) (chan []byte, chan []byte, chan bool, error) {
	// create a tmp dir and copy the nodejs script
	stdout := make(chan []byte)
	stderr := make(chan []byte)

	write_out := func(data []byte) {
		stdout <- data
	}

	write_err := func(data []byte) {
		stderr <- data
	}

	done_chan := make(chan bool)

	err := p.WithTempDir([]string{
		"/tmp/sandbox-nodejs-project/node_temp",
		"/tmp/sandbox-nodejs/nodejs.so",
	}, func(root_path string) error {
		// join nodejs_sandbox_fs and code
		code = string(nodejs_sandbox_fs) + code

		// override root_path/tmp/sandbox-nodejs-project/prescript.js
		script_path := path.Join(root_path, "tmp/sandbox-nodejs-project/node_temp/node_temp/test.js")
		err := os.WriteFile(script_path, []byte(code), 0755)
		if err != nil {
			return err
		}

		// create a new process
		cmd := exec.Command(
			static.GetDifySandboxGlobalConfigurations().NodejsPath,
			script_path,
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
				if strings.Contains(exit_string, "bad system call") {
					write_err([]byte("error: operation not permitted\n"))
				} else {
					write_err([]byte(fmt.Sprintf("error: %v\n", status.String())))
				}
			}

			// wait for the stdout and stderr to finish
			wg.Wait()
			stderr_reader.Close()
			stdout_reader.Close()
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
