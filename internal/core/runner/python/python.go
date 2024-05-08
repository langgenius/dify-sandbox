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
	}
)

func (p *PythonRunner) Run(
	code string,
	timeout time.Duration,
	stdin []byte,
	preload string,
	options types.RunnerOptions,
) (chan []byte, chan []byte, chan bool, error) {
	// initialize the environment
	untrusted_code_path, preload_script, err := p.InitializeEnvironment(code, preload)
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
			static.GetDifySandboxGlobalConfigurations().PythonPath,
			"-c",
			preload_script,
			untrusted_code_path,
			strconv.Itoa(static.SANDBOX_USER_UID),
			strconv.Itoa(static.SANDBOX_GROUP_ID),
			options.Json(),
		)
		cmd.Env = []string{}

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

func (p *PythonRunner) InitializeEnvironment(code string, preload string) (string, string, error) {
	// create a tmp dir and copy the python script
	temp_code_name := strings.ReplaceAll(uuid.New().String(), "-", "_")
	temp_code_name = strings.ReplaceAll(temp_code_name, "/", ".")

	untrusted_code_path := fmt.Sprintf("/tmp/code/%s.py", temp_code_name)
	err := os.MkdirAll("/tmp/code", 0755)
	if err != nil {
		return "", "", err
	}

	err = os.WriteFile(untrusted_code_path, []byte(code), 0755)
	if err != nil {
		return "", "", err
	}

	preload_script := string(python_sandbox_fs)
	if preload != "" {
		preload_script = fmt.Sprintf("%s\n%s", preload, preload_script)
	}

	return untrusted_code_path, preload_script, nil
}
