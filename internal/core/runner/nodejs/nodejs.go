package nodejs

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner"
	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/static"
)

type NodeJsRunner struct {
	runner.TempDirRunner
}

//go:embed prescript.js
var nodejs_sandbox_fs []byte

var (
	REQUIRED_FS = []string{
		path.Join(LIB_PATH, PROJECT_NAME, "node_temp"),
		path.Join(LIB_PATH, LIB_NAME),
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
	configuration := static.GetDifySandboxGlobalConfigurations()

	// capture the output
	output_handler := runner.NewOutputCaptureRunner()
	output_handler.SetTimeout(timeout)

	err := p.WithTempDir("/", REQUIRED_FS, func(root_path string) error {
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

		if len(configuration.AllowedSyscalls) > 0 {
			cmd.Env = append(
				cmd.Env,
				fmt.Sprintf("ALLOWED_SYSCALLS=%s", strings.Trim(
					strings.Join(strings.Fields(fmt.Sprint(configuration.AllowedSyscalls)), ","), "[]",
				)),
			)
		}

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
	if !checkLibAvaliable() {
		releaseLibBinary()
	}

	node_sandbox_file := string(nodejs_sandbox_fs)
	if preload != "" {
		node_sandbox_file = fmt.Sprintf("%s\n%s", preload, node_sandbox_file)
	}

	// join nodejs_sandbox_fs and code
	code = node_sandbox_file + code

	// override root_path/tmp/sandbox-nodejs-project/prescript.js
	script_path := path.Join(root_path, LIB_PATH, PROJECT_NAME, "node_temp/node_temp/test.js")
	err := os.WriteFile(script_path, []byte(code), 0755)
	if err != nil {
		return "", err
	}

	return script_path, nil
}
