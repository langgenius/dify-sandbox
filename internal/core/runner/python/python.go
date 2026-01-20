package python

import (
	"context"
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
	ctx context.Context,
	code string,
	timeout time.Duration,
	stdin []byte,
	preload string,
	options *types.RunnerOptions,
) (chan []byte, chan []byte, chan bool, error) {
	configuration := static.GetDifySandboxGlobalConfigurations()

	// initialize the environment
	untrustedCodePath, key, err := p.InitializeEnvironment(code, preload, options)
	if err != nil {
		return nil, nil, nil, err
	}

	// capture the output
	outputHandler := runner.NewOutputCaptureRunner()
	outputHandler.SetTimeout(timeout)
	outputHandler.SetAfterExitHook(func() {
		// remove untrusted code
		os.Remove(untrustedCodePath)
	})

	// create a new process
	cmd := exec.Command(
		configuration.PythonPath,
		untrustedCodePath,
		LIB_PATH,
		key,
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

	err = outputHandler.CaptureOutput(ctx, cmd)
	if err != nil {
		return nil, nil, nil, err
	}

	return outputHandler.GetStdout(), outputHandler.GetStderr(), outputHandler.GetDone(), nil
}

func (p *PythonRunner) InitializeEnvironment(code string, preload string, options *types.RunnerOptions) (string, string, error) {
	if !checkLibAvaliable() {
		// ensure environment is reversed
		releaseLibBinary(false)
	}

	// create a tmp dir and copy the python script
	tempCodeName := strings.ReplaceAll(uuid.New().String(), "-", "_")
	tempCodeName = strings.ReplaceAll(tempCodeName, "/", ".")

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
	encodedKey := base64.StdEncoding.EncodeToString(key)

	code = strings.Replace(
		script,
		"{{code}}",
		code,
		1,
	)

	untrustedCodePath := fmt.Sprintf("%s/tmp/%s.py", LIB_PATH, tempCodeName)
	err = os.MkdirAll(path.Dir(untrustedCodePath), 0755)
	if err != nil {
		return "", "", err
	}
	err = os.WriteFile(untrustedCodePath, []byte(code), 0755)
	if err != nil {
		return "", "", err
	}

	return untrustedCodePath, encodedKey, nil
}
