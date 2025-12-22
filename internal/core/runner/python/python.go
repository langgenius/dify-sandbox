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

	"github.com/google/uuid"
	"github.com/langgenius/dify-sandbox/internal/core/runner"
	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/static"
)

type PythonRunner struct {
	runner.TempDirRunner
}

//go:embed prescript.py
var sandbox_fs []byte

func (p *PythonRunner) Run(
	code string,
	timeout time.Duration,
	stdin []byte,
	preload string,
	options *types.RunnerOptions,
) (chan []byte, chan []byte, chan bool, error) {
	configuration := static.GetDifySandboxGlobalConfigurations()

	// initialize the environment
	untrusted_code_path, key, err := p.InitializeEnvironment(code, preload, options)
	if err != nil {
		return nil, nil, nil, err
	}

	// capture the output
	output_handler := runner.NewOutputCaptureRunner()
	output_handler.SetTimeout(timeout)
	output_handler.SetAfterExitHook(func() {
		// remove the entire run directory
		os.RemoveAll(path.Dir(untrusted_code_path))
	})

    // calculate runDir from untrusted_code_path
    runDir := path.Dir(untrusted_code_path)
    // untrusted_code_path is .../tmp/<uuid>/<uuid>.py
    // runDir is .../tmp/<uuid>
    runID := path.Base(runDir)
    relRunDir := path.Join("tmp", runID)

	// create a new process
	cmd := exec.Command(
		configuration.PythonPath,
		untrusted_code_path,
		LIB_PATH,
		key,
        relRunDir,
	)
	cmd.Env = []string{}
	cmd.Dir = LIB_PATH

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

	if len(configuration.AllowedSyscalls) > 0 {
		cmd.Env = append(cmd.Env,
			fmt.Sprintf("ALLOWED_SYSCALLS=%s",
				strings.Trim(strings.Join(strings.Fields(fmt.Sprint(configuration.AllowedSyscalls)), ","), "[]"),
			),
		)
	}

	err = output_handler.CaptureOutput(cmd)
	if err != nil {
		return nil, nil, nil, err
	}

	return output_handler.GetStdout(), output_handler.GetStderr(), output_handler.GetDone(), nil
}

func (p *PythonRunner) InitializeEnvironment(code string, preload string, options *types.RunnerOptions) (string, string, error) {
	if !checkLibAvaliable() {
		// ensure environment is reversed
		releaseLibBinary(false)
	}

	// create a tmp dir and copy the python script
	temp_code_name := strings.ReplaceAll(uuid.New().String(), "-", "_")
	temp_code_name = strings.ReplaceAll(temp_code_name, "/", ".")

	// Create a unique directory for this run
	runDir := path.Join(LIB_PATH, "tmp", temp_code_name)
	err := os.MkdirAll(runDir, 0755)
	if err != nil {
		return "", "", err
	}

	// Write uploaded files
    if options.Files != nil {
        for filename, content := range options.Files {
            filePath := path.Join(runDir, filename)
             // ensure strict path safety to prevent directory traversal
             if !strings.HasPrefix(path.Clean(filePath), runDir) {
                 continue
             }
             // Ensure parent dir exists
             os.MkdirAll(path.Dir(filePath), 0755)
             os.WriteFile(filePath, []byte(content), 0644)
        }
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
	_, err = rand.Read(key)
	if err != nil {
		return "", "", err
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

    // Write the prescript (untrusted code) to the run directory
	untrusted_code_path := path.Join(runDir, fmt.Sprintf("%s.py", temp_code_name))
	err = os.WriteFile(untrusted_code_path, []byte(code), 0755)
	if err != nil {
		return "", "", err
	}

	return untrusted_code_path, encoded_key, nil
}
