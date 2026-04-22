package python

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"log/slog"
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

	bootstrapPath, err := p.InitializeEnvironment(preload, options, uid)
	if err != nil {
		ReleaseUID(uid)
		return nil, nil, nil, err
	}

	codeReader, codeWriter, err := os.Pipe()
	if err != nil {
		os.Remove(bootstrapPath)
		ReleaseUID(uid)
		return nil, nil, nil, err
	}

	outputHandler := runner.NewOutputCaptureRunner()
	outputHandler.SetTimeout(timeout)
	outputHandler.SetAfterExitHook(func() {
		codeReader.Close()
		codeWriter.Close()
		os.Remove(bootstrapPath)
		ReleaseUID(uid)
	})

	// create a new process
	cmd := exec.Command(
		configuration.PythonPath,
		bootstrapPath,
		LIB_PATH,
	)
	cmd.Env = []string{}
	cmd.Dir = LIB_PATH
	cmd.ExtraFiles = []*os.File{codeReader}

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

	if configuration.AllowedSyscallFilePath != "" {
		if _, err := os.Stat(configuration.AllowedSyscallFilePath); err == nil {
			content, _ := os.ReadFile(configuration.AllowedSyscallFilePath)

			parts := strings.Split(strings.TrimSpace(string(content)), ",")
			var numbers []int
			for _, part := range parts {
				if part == "" {
					continue
				}
				num, _ := strconv.Atoi(strings.TrimSpace(part))
				numbers = append(numbers, num)
			}
			if len(numbers) > 0 {
				configuration.AllowedSyscalls = append(configuration.AllowedSyscalls, numbers...)
				slog.Info("config syscall length", "info", len(configuration.AllowedSyscalls))
			}
		} else {
			slog.Error("file not exists", "err", err, "file path", configuration.AllowedSyscallFilePath)
		}
	}

	if len(configuration.AllowedSyscalls) > 0 {
		cmd.Env = append(cmd.Env,
			fmt.Sprintf("ALLOWED_SYSCALLS=%s",
				strings.Trim(strings.Join(strings.Fields(fmt.Sprint(configuration.AllowedSyscalls)), ","), "[]"),
			),
		)
	}

	go func() {
		_, _ = io.WriteString(codeWriter, code)
		codeWriter.Close()
	}()

	err = outputHandler.CaptureOutput(ctx, cmd)
	if err != nil {
		codeReader.Close()
		codeWriter.Close()
		os.Remove(bootstrapPath)
		ReleaseUID(uid)
		return nil, nil, nil, err
	}

	return outputHandler.GetStdout(), outputHandler.GetStderr(), outputHandler.GetDone(), nil
}

func buildBootstrap(preload string, options *types.RunnerOptions, uid int) string {
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

	return strings.Replace(
		script,
		"{{preload}}",
		fmt.Sprintf("%s\n", preload),
		1,
	)
}

func (p *PythonRunner) InitializeEnvironment(preload string, options *types.RunnerOptions, uid int) (string, error) {
	if !checkLibAvaliable() {
		releaseLibBinary(false)
	}

	tempCodeName := strings.ReplaceAll(uuid.New().String(), "-", "_")
	tempCodeName = strings.ReplaceAll(tempCodeName, "/", ".")

	script := buildBootstrap(preload, options, uid)

	bootstrapPath := fmt.Sprintf("%s/tmp/%s.py", LIB_PATH, tempCodeName)
	err := os.MkdirAll(path.Dir(bootstrapPath), 0755)
	if err != nil {
		return "", err
	}
	err = os.WriteFile(bootstrapPath, []byte(script), 0600)
	if err != nil {
		return "", err
	}
	if err = syscall.Chown(bootstrapPath, uid, static.SANDBOX_GROUP_ID); err != nil {
		os.Remove(bootstrapPath)
		return "", fmt.Errorf("chown script to uid %d: %w", uid, err)
	}

	return bootstrapPath, nil
}
