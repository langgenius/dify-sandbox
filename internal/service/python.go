package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner/python"
	runner_types "github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/static"
	"github.com/langgenius/dify-sandbox/internal/types"
	appLog "github.com/langgenius/dify-sandbox/internal/utils/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

type RunCodeResponse struct {
	Stderr string `json:"error"`
	Stdout string `json:"stdout"`
}

func RunPython3Code(ctx context.Context, code string, preload string, options *runner_types.RunnerOptions) *types.DifySandboxResponse {
	if err := checkOptions(options); err != nil {
		return types.ErrorResponse(-400, err.Error())
	}

	if !static.GetDifySandboxGlobalConfigurations().EnablePreload {
		preload = ""
	}

	timeout := time.Duration(
		static.GetDifySandboxGlobalConfigurations().WorkerTimeout * int(time.Second),
	)

	tr := otel.Tracer("dify-sandbox/service")
	ctx, span := tr.Start(ctx, "python.run")
	span.SetAttributes(
		attribute.Int("code.length", len(code)),
		attribute.Bool("options.enable_network", options != nil && options.EnableNetwork),
		attribute.Int64("timeout.ms", int64(timeout/time.Millisecond)),
	)
	if id, ok := appLog.IdentityFromContext(ctx); ok && id.TenantID != "" {
		span.SetAttributes(attribute.String("tenant.id", id.TenantID))
	}
	defer span.End()

	runner := python.PythonRunner{}
	stdout, stderr, done, err := runner.Run(
		ctx, code, timeout, nil, preload, options,
	)
	if err != nil {
		if errors.Is(err, python.ErrUIDPoolExhausted) {
			return types.ErrorResponse(-429, err.Error())
		}
		return types.ErrorResponse(-500, err.Error())
	}

	var stdoutStr strings.Builder
	var stderrStr strings.Builder

	defer close(done)

	for {
		select {
		case <-done:
			// Drain any remaining buffered output to avoid races
		drain:
			for {
				select {
				case out := <-stdout:
					stdoutStr.Write(out)
				case err := <-stderr:
					stderrStr.Write(err)
				default:
					break drain
				}
			}
			// Annotate span with sizes
			span.SetAttributes(
				attribute.Int("stdout.length", stdoutStr.Len()),
				attribute.Int("stderr.length", stderrStr.Len()),
			)
			return types.SuccessResponse(&RunCodeResponse{
				Stdout: stdoutStr.String(),
				Stderr: stderrStr.String(),
			})
		case out := <-stdout:
			stdoutStr.Write(out)
		case err := <-stderr:
			stderrStr.Write(err)
		}
	}
}

type ListDependenciesResponse struct {
	Dependencies []runner_types.Dependency `json:"dependencies"`
}

func ListPython3Dependencies() *types.DifySandboxResponse {
	return types.SuccessResponse(&ListDependenciesResponse{
		Dependencies: python.ListDependencies(),
	})
}

type RefreshDependenciesResponse struct {
	Dependencies []runner_types.Dependency `json:"dependencies"`
}

func RefreshPython3Dependencies() *types.DifySandboxResponse {
	return types.SuccessResponse(&RefreshDependenciesResponse{
		Dependencies: python.RefreshDependencies(),
	})
}

type UpdateDependenciesResponse struct{}

func UpdateDependencies() *types.DifySandboxResponse {
	err := python.PreparePythonDependenciesEnv()
	if err != nil {
		return types.ErrorResponse(-500, err.Error())
	}

	return types.SuccessResponse(&UpdateDependenciesResponse{})
}
