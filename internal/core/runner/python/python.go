package python

import (
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

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

	if !checkLibAvaliable() {
		releaseLibBinary(false)
	}

	// prepare stdin payload
	payload, encodedKey, err := p.prepareStdinPayload(code, preload, options)
	if err != nil {
		return nil, nil, nil, err
	}

	// capture the output
	outputHandler := runner.NewOutputCaptureRunner()
	outputHandler.SetTimeout(timeout)

	// create a new process
	cmd := exec.Command(
		configuration.PythonPath,
		fmt.Sprintf("%s/%s", LIB_PATH, PRESCRIPT_NAME),
		LIB_PATH,
		encodedKey,
		strconv.Itoa(static.SANDBOX_USER_UID),
		strconv.Itoa(static.SANDBOX_GROUP_ID),
	)
	cmd.Env = []string{}
	cmd.Dir = LIB_PATH
	cmd.Stdin = bytes.NewReader(payload)

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

func (p *PythonRunner) prepareStdinPayload(code string, preload string, options *types.RunnerOptions) ([]byte, string, error) {
	// generate a random 512 bit key
	keyLen := 64
	key := make([]byte, keyLen)
	_, err := rand.Read(key)
	if err != nil {
		return nil, "", err
	}

	// encrypt the code with XOR
	encryptedCode := make([]byte, len(code))
	for i := 0; i < len(code); i++ {
		encryptedCode[i] = code[i] ^ key[i%keyLen]
	}

	// encode key using base64
	encodedKey := base64.StdEncoding.EncodeToString(key)

	// base64-encode the three payload lines
	enableNetworkFlag := "0"
	if options.EnableNetwork {
		enableNetworkFlag = "1"
	}
	enableNetworkB64 := base64.StdEncoding.EncodeToString([]byte(enableNetworkFlag))
	preloadB64 := base64.StdEncoding.EncodeToString([]byte(preload))
	codeB64 := base64.StdEncoding.EncodeToString(encryptedCode)

	// build stdin payload: 3 base64-encoded lines
	payload := []byte(enableNetworkB64 + "\n" + preloadB64 + "\n" + codeB64 + "\n")

	return payload, encodedKey, nil
}
