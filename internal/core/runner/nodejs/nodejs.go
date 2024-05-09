package nodejs

import (
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strconv"
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner"
	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/static"
	"github.com/langgenius/dify-sandbox/internal/utils/log"
)

type NodeJsRunner struct {
	runner.TempDirRunner
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

var (
	NODEJS_REQUIRED_FS = []string{
		"/tmp/sandbox-nodejs-project/node_temp",
		"/tmp/sandbox-nodejs/nodejs.so",
		"/etc/ssl/certs/ca-certificates.crt",
		"/etc/nsswitch.conf",
		"/etc/resolv.conf",
		"/run/systemd/resolve/stub-resolv.conf",
		"/etc/hosts",
	}
)

func (p *NodeJsRunner) Run(
	code string,
	timeout time.Duration,
	stdin []byte,
	preload string,
	options *types.RunnerOptions,
) (chan []byte, chan []byte, chan bool, error) {
	// capture the output
	output_handler := runner.NewOutputCaptureRunner()
	output_handler.SetTimeout(timeout)

	err := p.WithTempDir(NODEJS_REQUIRED_FS, func(root_path string) error {
		output_handler.SetAfterExitHook(func() {
			os.RemoveAll(root_path)
			os.Remove(root_path)
		})

		// initialize the environment
		script_path, err := p.InitializeEnvironment(code, preload, root_path)
		if err != nil {
			return err
		}

		// create a new process
		cmd := exec.Command(
			static.GetDifySandboxGlobalConfigurations().NodejsPath,
			script_path,
			strconv.Itoa(static.SANDBOX_USER_UID),
			strconv.Itoa(static.SANDBOX_GROUP_ID),
			options.Json(),
		)
		cmd.Env = []string{}

		// capture the output
		err = output_handler.CaptureOutput(cmd)
		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return nil, nil, nil, err
	}

	return output_handler.GetStdout(), output_handler.GetStderr(), output_handler.GetDone(), nil
}

func (p *NodeJsRunner) InitializeEnvironment(code string, preload string, root_path string) (string, error) {
	node_sandbox_file := string(nodejs_sandbox_fs)
	if preload != "" {
		node_sandbox_file = fmt.Sprintf("%s\n%s", preload, node_sandbox_file)
	}

	// join nodejs_sandbox_fs and code
	code = node_sandbox_file + code

	// override root_path/tmp/sandbox-nodejs-project/prescript.js
	script_path := path.Join(root_path, "tmp/sandbox-nodejs-project/node_temp/node_temp/test.js")
	err := os.WriteFile(script_path, []byte(code), 0755)
	if err != nil {
		return "", err
	}

	return script_path, nil
}
