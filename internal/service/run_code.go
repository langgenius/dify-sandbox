package service

import (
	"fmt"
	"strings"
)

type codeOutputResult interface {
	GetStdout() chan []byte
	GetStderr() chan []byte
	GetExecError() chan []byte
	GetDone() chan bool
	GetExitCode() int
}

// RunCodeResponse is the public /v1/sandbox/run data payload.
// Stdout is raw process stdout, Stderr is raw process stderr/log output,
// Error is reserved for actual execution failures, and ExitCode is the
// process exit status. Successful executions keep Error empty even when
// Stderr contains logging or other non-fatal stderr output.
type RunCodeResponse struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	Error    string `json:"error"`
	ExitCode int    `json:"exit_code"`
}

func collectRunCodeResponse(result codeOutputResult) *RunCodeResponse {
	var stdoutStr strings.Builder
	var stderrStr strings.Builder
	var execErrorStr strings.Builder

	for {
		select {
		case <-result.GetDone():
			for {
				select {
				case out := <-result.GetStdout():
					stdoutStr.Write(out)
				case err := <-result.GetStderr():
					stderrStr.Write(err)
				case execErr := <-result.GetExecError():
					execErrorStr.Write(execErr)
				default:
					exitCode := result.GetExitCode()
					stderr := stderrStr.String()
					return &RunCodeResponse{
						Stdout:   stdoutStr.String(),
						Stderr:   stderr,
						Error:    buildExecutionError(exitCode, stderr, execErrorStr.String()),
						ExitCode: exitCode,
					}
				}
			}
		case out := <-result.GetStdout():
			stdoutStr.Write(out)
		case err := <-result.GetStderr():
			stderrStr.Write(err)
		case execErr := <-result.GetExecError():
			execErrorStr.Write(execErr)
		}
	}
}

func buildExecutionError(exitCode int, stderr string, execError string) string {
	if exitCode == 0 {
		return execError
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("process exited with code %d", exitCode))
	appendExecutionErrorDetail(&builder, execError)
	appendExecutionErrorDetail(&builder, stderr)

	return builder.String()
}

func appendExecutionErrorDetail(builder *strings.Builder, detail string) {
	if detail == "" {
		return
	}

	if builder.Len() > 0 && !strings.HasSuffix(builder.String(), "\n") {
		builder.WriteByte('\n')
	}

	builder.WriteString(detail)
}
