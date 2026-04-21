package nodejs

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner"
	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/core/runner/uidpool"
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
	ctx context.Context,
	code string,
	timeout time.Duration,
	stdin []byte,
	preload string,
	options *types.RunnerOptions,
) (chan []byte, chan []byte, chan bool, error) {
	configuration := static.GetDifySandboxGlobalConfigurations()

	uid, err := uidpool.AcquireUID(ctx)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("no available sandbox UID: %w", err)
	}
	releaseUID := true

	// capture the output
	output_handler := runner.NewOutputCaptureRunner()
	output_handler.SetTimeout(timeout)

	err = p.WithTempDir("/", REQUIRED_FS, func(root_path string) error {
		cleanupRootPath := true
		defer func() {
			if cleanupRootPath {
				os.RemoveAll(root_path)
			}
		}()

		// initialize the environment
		script_path, err := p.InitializeEnvironment(preload, root_path)
		if err != nil {
			return err
		}

		codeReader, codeWriter, err := os.Pipe()
		if err != nil {
			return err
		}
		output_handler.SetAfterExitHook(func() {
			codeReader.Close()
			codeWriter.Close()
			os.RemoveAll(root_path)
			uidpool.ReleaseUID(uid)
		})

		// create a new process
		cmd := exec.Command(configuration.NodejsPath, buildCommandArgs(script_path, uid, options)...)
		cmd.Env = []string{}
		cmd.ExtraFiles = []*os.File{codeReader}

		if len(configuration.AllowedSyscalls) > 0 {
			cmd.Env = append(
				cmd.Env,
				fmt.Sprintf("ALLOWED_SYSCALLS=%s", strings.Trim(
					strings.Join(strings.Fields(fmt.Sprint(configuration.AllowedSyscalls)), ","), "[]",
				)),
			)
		}

		go func() {
			_, _ = io.WriteString(codeWriter, code)
			codeWriter.Close()
		}()

		// capture the output
		err = output_handler.CaptureOutput(ctx, cmd)
		if err != nil {
			codeReader.Close()
			codeWriter.Close()
			return err
		}
		releaseUID = false
		cleanupRootPath = false

		return nil
	})

	if err != nil {
		if releaseUID {
			uidpool.ReleaseUID(uid)
		}
		return nil, nil, nil, err
	}

	return output_handler.GetStdout(), output_handler.GetStderr(), output_handler.GetDone(), nil
}

func buildCommandArgs(scriptPath string, uid int, options *types.RunnerOptions) []string {
	return []string{
		scriptPath,
		strconv.Itoa(uid),
		strconv.Itoa(static.SANDBOX_GROUP_ID),
		options.Json(),
	}
}

func buildBootstrap(preload string) string {
	node_sandbox_file := string(nodejs_sandbox_fs)
	if preload != "" {
		node_sandbox_file = fmt.Sprintf("%s\n%s", preload, node_sandbox_file)
	}

	return node_sandbox_file
}

func (p *NodeJsRunner) InitializeEnvironment(preload string, root_path string) (string, error) {
	if !checkLibAvaliable() {
		releaseLibBinary()
	}

	code := buildBootstrap(preload)

	// override root_path/tmp/sandbox-nodejs-project/prescript.js
	script_path := path.Join(root_path, LIB_PATH, PROJECT_NAME, "node_temp/node_temp/test.js")
	err := os.WriteFile(script_path, []byte(code), 0755)
	if err != nil {
		return "", err
	}

	return script_path, nil
}
