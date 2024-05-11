package python

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/langgenius/dify-sandbox/internal/core/runner"
	python_dependencies "github.com/langgenius/dify-sandbox/internal/core/runner/python/dependencies"
	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/static"
)

type PythonRunner struct {
	runner.TempDirRunner
}

//go:embed prescript.py
var python_sandbox_fs []byte

var (
	PYTHON_REQUIRED_FS = []string{
		"/tmp/sandbox-python/python.so",
		"/etc/ssl/certs/ca-certificates.crt",
		"/usr/local/lib/python3.10/site-packages/certifi/cacert.pem",
		"/usr/local/lib/python3.10/dist-packages/certifi/cacert.pem",
		"/etc/nsswitch.conf",
		"/etc/resolv.conf",
		"/run/systemd/resolve/stub-resolv.conf",
		"/run/resolvconf/resolv.conf",
		"/etc/hosts",
	}
)

func (p *PythonRunner) Run(
	code string,
	timeout time.Duration,
	stdin []byte,
	preload string,
	options *types.RunnerOptions,
) (chan []byte, chan []byte, chan bool, error) {
	configuration := static.GetDifySandboxGlobalConfigurations()

	// initialize the environment
	untrusted_code_path, err := p.InitializeEnvironment(code, preload, options)
	if err != nil {
		return nil, nil, nil, err
	}

	// capture the output
	output_handler := runner.NewOutputCaptureRunner()
	output_handler.SetTimeout(timeout)

	err = p.WithTempDir(PYTHON_REQUIRED_FS, func(root_path string) error {
		// cleanup
		output_handler.SetAfterExitHook(func() {
			os.RemoveAll(root_path)
			os.Remove(root_path)
		})

		// create a new process
		cmd := exec.Command(
			configuration.PythonPath,
			untrusted_code_path,
		)
		cmd.Env = []string{}

		if configuration.Proxy.Socks5 != "" {
			cmd.Env = append(cmd.Env, fmt.Sprintf("HTTPS_PROXY=%s", configuration.Proxy.Socks5))
			cmd.Env = append(cmd.Env, fmt.Sprintf("HTTP_PROXY=%s", configuration.Proxy.Socks5))
		} else if configuration.Proxy.Https != "" || configuration.Proxy.Http != "" {
			if configuration.Proxy.Https != "" {
				cmd.Env = append(cmd.Env, fmt.Sprintf("HTTPS_PROXY=%s", configuration.Proxy.Https))
			}
			if configuration.Proxy.Http != "" {
				cmd.Env = append(cmd.Env, fmt.Sprintf("HTTP_PROXY=%s", configuration.Proxy.Http))
			}
		}

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

func (p *PythonRunner) InitializeEnvironment(code string, preload string, options *types.RunnerOptions) (string, error) {
	// create a tmp dir and copy the python script
	temp_code_name := strings.ReplaceAll(uuid.New().String(), "-", "_")
	temp_code_name = strings.ReplaceAll(temp_code_name, "/", ".")

	packages_preload := make([]string, len(options.Dependencies))
	for i, dependency := range options.Dependencies {
		packages_preload[i] = python_dependencies.GetDependencies(dependency.Name, dependency.Version)
	}

	script := strings.Replace(
		string(python_sandbox_fs),
		"{{uid}}", strconv.Itoa(static.SANDBOX_USER_UID), 1,
	)

	script = strings.Replace(
		script,
		"{{gid}}", strconv.Itoa(static.SANDBOX_GROUP_ID), 1,
	)

	if options.EnableNetwork {
		script = strings.Replace(
			script,
			"{{enable_network}}", "1", 1,
		)
	} else {
		script = strings.Replace(
			script,
			"{{enable_network}}", "0", 1,
		)
	}

	script = strings.Replace(
		script,
		"{{preload}}",
		fmt.Sprintf("%s\n%s", preload, strings.Join(packages_preload, "\n")),
		1,
	)

	code = strings.Replace(
		script,
		"{{code}}",
		code,
		1,
	)

	untrusted_code_path := fmt.Sprintf("/tmp/code/%s.py", temp_code_name)
	err := os.MkdirAll("/tmp/code", 0755)
	if err != nil {
		return "", err
	}
	err = os.WriteFile(untrusted_code_path, []byte(code), 0755)
	if err != nil {
		return "", err
	}

	return untrusted_code_path, nil
}
