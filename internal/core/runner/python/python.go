package python

import (
	"crypto/rand"
	_ "embed"
	"encoding/base64"
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
	"github.com/langgenius/dify-sandbox/internal/utils/log"
)

type PythonRunner struct {
	runner.TempDirRunner
}

//go:embed prescript.py
var sandbox_fs []byte

var (
	PYTHON_ENTRY_NAME = "main.py"
	PYTHON_VENV_NAME  = ".venv"
)

func SetPythonEnvironment(base_path string, cmd *exec.Cmd) {
	configuration := static.GetDifySandboxGlobalConfigurations()
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
}

func (p *PythonRunner) Run(
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

	err := p.WithTempDir(LIB_PATH, []string{}, func(base_path string) error {
		output_handler.SetAfterExitHook(func() {
			// remove untrusted code
			os.Remove(base_path)
		})
		// initialize the environment
		key, err := p.InitializeEnvironment(base_path, code, preload, options)
		if err != nil {
			return err
		}

		// if we have custom dependencies, we have to use virtual environment
		// otherwise, we could use system python directly to reduce startup overhead
		python_path := configuration.PythonPath
		if len(options.Dependencies) > 0 {
			// create virtual environment
			err = p.CreateVirtualEnvironment(base_path, options.Dependencies)
			if err != nil {
				return err
			}
			python_path = path.Join(base_path, PYTHON_VENV_NAME, "bin", "python")
		}

		// create a new process
		cmd := exec.Command(
			python_path,
			path.Join(base_path, PYTHON_ENTRY_NAME),
			// remain on lib path, we need system libraries, these are init on sandbox startup
			LIB_PATH,
			key,
		)
		cmd.Env = []string{}
		SetPythonEnvironment(base_path, cmd)
		// remain on lib path, we need system libraries, these are init on sandbox startup
		cmd.Dir = LIB_PATH

		if len(configuration.AllowedSyscalls) > 0 {
			cmd.Env = append(cmd.Env,
				fmt.Sprintf("ALLOWED_SYSCALLS=%s",
					strings.Trim(strings.Join(strings.Fields(fmt.Sprint(configuration.AllowedSyscalls)), ","), "[]"),
				),
			)
		}

		return output_handler.CaptureOutput(cmd)
	})

	if err != nil {
		return nil, nil, nil, err
	}

	return output_handler.GetStdout(), output_handler.GetStderr(), output_handler.GetDone(), nil
}

func (p *PythonRunner) InitializeEnvironment(base_path string, code string, preload string, options *types.RunnerOptions) (string, error) {
	if !checkLibAvaliable() {
		// ensure environment is reversed
		releaseLibBinary(false)
	}

	script := strings.Replace(
		string(sandbox_fs),
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
		fmt.Sprintf("%s\n", preload),
		1,
	)

	// generate a random 512 bit key
	key_len := 64
	key := make([]byte, key_len)
	_, err := rand.Read(key)
	if err != nil {
		return "", err
	}

	// encrypt the code
	encrypted_code := make([]byte, len(code))
	for i := 0; i < len(code); i++ {
		encrypted_code[i] = code[i] ^ key[i%key_len]
	}

	// encode code using base64
	code = base64.StdEncoding.EncodeToString(encrypted_code)
	// encode key using base64
	encoded_key := base64.StdEncoding.EncodeToString(key)

	code = strings.Replace(
		script,
		"{{code}}",
		code,
		1,
	)

	err = os.WriteFile(path.Join(base_path, PYTHON_ENTRY_NAME), []byte(code), 0755)
	if err != nil {
		return "", err
	}

	return encoded_key, nil
}

func (p *PythonRunner) CreateVirtualEnvironment(base_path string, dependencies []types.Dependency) error {
	// we use uv to manage libraries, which is faster than venv
	// init project
	cmd := exec.Command("uv", "init", "-q", "--bare")
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Error("failed to initialize uv: %v, output: %s", err.Error(), string(output))
		return err
	}

	// create venv
	// we allow system site packages for compatibility with existing functionalities
	cmd = exec.Command("uv", "venv", path.Join(base_path, PYTHON_VENV_NAME), "-q", "--system-site-packages")
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Error("failed to create venv: %v, output: %s", err.Error(), string(output))
		return err
	}

	if len(dependencies) == 0 {
		return nil
	}

	// install dependencies
	args := []string{"add", "-q"}
	pipMirrorURL := static.GetDifySandboxGlobalConfigurations().PythonPipMirrorURL
	if pipMirrorURL != "" {
		// If a mirror URL is provided, include it in the command arguments
		args = append(args, "-i", pipMirrorURL)
	}

	for _, dependency := range dependencies {
		if dependency.Version != "" {
			// if version contains constraints, use it directly
			// according to PEP 508, searching '=', '>', '<' is enough
			// see https://peps.python.org/pep-0508/#complete-grammar
			if strings.Contains(dependency.Version, "=") ||
				strings.Contains(dependency.Version, "<") ||
				strings.Contains(dependency.Version, ">") {
				args = append(args, dependency.Name+dependency.Version)
			} else {
				args = append(args, dependency.Name+"=="+dependency.Version)
			}
		} else {
			args = append(args, dependency.Name)
		}
	}

	cmd = exec.Command("uv", args...)
	cmd.Env = []string{}
	SetPythonEnvironment(base_path, cmd)
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Error("failed to install dependencies: %v, output: %s", err.Error(), string(output))
		return err
	}

	return nil
}
