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
	"syscall"
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

	uid, err := AcquireUID(ctx)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("no available sandbox UID: %w", err)
	}

	untrustedCodePath, key, err := p.InitializeEnvironment(code, preload, options, uid)
	if err != nil {
		ReleaseUID(uid)
		return nil, nil, nil, err
	}

	outputHandler := runner.NewOutputCaptureRunner()
	outputHandler.SetTimeout(timeout)
	outputHandler.SetAfterExitHook(func() {
		os.Remove(untrustedCodePath)
		ReleaseUID(uid)
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
		os.Remove(untrustedCodePath)
		ReleaseUID(uid)
		return nil, nil, nil, err
	}

	return outputHandler.GetStdout(), outputHandler.GetStderr(), outputHandler.GetDone(), nil
}

func (p *PythonRunner) InitializeEnvironment(code string, preload string, options *types.RunnerOptions, uid int) (string, string, error) {
	if !checkLibAvaliable() {
		releaseLibBinary(false)
	}

	tempCodeName := strings.ReplaceAll(uuid.New().String(), "-", "_")
	tempCodeName = strings.ReplaceAll(tempCodeName, "/", ".")

	script := strings.Replace(
		string(sandbox_fs),
		"{{uid}}", strconv.Itoa(uid), 1,
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
	err = os.WriteFile(untrustedCodePath, []byte(code), 0600)
	if err != nil {
		return "", "", err
	}
	if err = syscall.Chown(untrustedCodePath, uid, static.SANDBOX_GROUP_ID); err != nil {
		os.Remove(untrustedCodePath)
		return "", "", fmt.Errorf("chown script to uid %d: %w", uid, err)
	}

	return untrustedCodePath, encodedKey, nil
}
