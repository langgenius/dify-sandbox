package nodejs

import (
	"context"
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
	"github.com/langgenius/dify-sandbox/internal/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
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

	// capture the output
	outputHandler := runner.NewOutputCaptureRunner()
	outputHandler.SetTimeout(timeout)

	err := p.WithTempDir("/", REQUIRED_FS, func(root_path string) error {
		outputHandler.SetAfterExitHook(func() {
			os.RemoveAll(root_path)
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

		// inject trace context into env (only if OTel enabled)
		if static.GetDifySandboxGlobalConfigurations().Otel.Enable {
			cmd.Env = append(cmd.Env, telemetry.InjectTraceEnv(ctx)...)
		}

		if len(configuration.AllowedSyscalls) > 0 {
			cmd.Env = append(
				cmd.Env,
				fmt.Sprintf("ALLOWED_SYSCALLS=%s", strings.Trim(
					strings.Join(strings.Fields(fmt.Sprint(configuration.AllowedSyscalls)), ","), "[]",
				)),
			)
		}

		tr := otel.Tracer("dify-sandbox/runner")
		_, span := tr.Start(ctx, "node.exec")
		span.SetAttributes(
			attribute.Int("code.length", len(code)),
			attribute.Int64("timeout.ms", int64(timeout/time.Millisecond)),
		)
		defer span.End()
		// capture the output
		err = outputHandler.CaptureOutput(ctx, cmd)
		if err != nil {
			span.RecordError(err)
			return err
		}
		return nil
	})

	if err != nil {
		return nil, nil, nil, err
	}

	return outputHandler.GetStdout(), outputHandler.GetStderr(), outputHandler.GetDone(), nil
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
	// encode code with base64
	code = base64.StdEncoding.EncodeToString([]byte(code))
	// FIXE: redeclared function causes code injection
	evalCode := fmt.Sprintf("eval(Buffer.from('%s', 'base64').toString('utf-8'))", code)
	code = node_sandbox_file + evalCode

	// override root_path/tmp/sandbox-nodejs-project/prescript.js
	script_path := path.Join(root_path, LIB_PATH, PROJECT_NAME, "node_temp/node_temp/test.js")
	err := os.WriteFile(script_path, []byte(code), 0755)
	if err != nil {
		return "", err
	}

	return script_path, nil
}
