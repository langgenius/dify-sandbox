package service

import (
	"bytes"
	"github.com/langgenius/dify-sandbox/internal/types"
)

func collectOutput(stdout, stderr chan []byte, done chan bool, limit int) *types.DifySandboxResponse {
	var out, errout bytes.Buffer
	failure := ""
	appendBounded := func(dst *bytes.Buffer, b []byte, reason string) {
		remaining := limit - dst.Len()
		if len(b) > remaining {
			failure = reason
			b = b[:remaining]
		}
		dst.Write(b)
	}
	for {
		select {
		case b := <-stdout:
			appendBounded(&out, b, "stdout_limit")
		case b := <-stderr:
			appendBounded(&errout, b, "stderr_limit")
		case success := <-done:
		drain:
			for {
				select {
				case b := <-stdout:
					appendBounded(&out, b, "stdout_limit")
				case b := <-stderr:
					appendBounded(&errout, b, "stderr_limit")
				default:
					break drain
				}
			}
			if !success || failure != "" {
				if failure == "" {
					failure = errout.String()
				}
				return types.ErrorResponse(-500, failure)
			}
			return types.SuccessResponse(&RunCodeResponse{Stdout: out.String(), Stderr: errout.String()})
		}
	}
}
