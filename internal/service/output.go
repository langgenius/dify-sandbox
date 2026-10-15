package service

import (
	"bytes"
	"github.com/langgenius/dify-sandbox/internal/types"
)

func collectOutput(stdout, stderr chan []byte, done chan bool) *types.DifySandboxResponse {
	var out, errout bytes.Buffer
	for {
		select {
		case b := <-stdout:
			out.Write(b)
		case b := <-stderr:
			errout.Write(b)
		case <-done:
		drain:
			for {
				select {
				case b := <-stdout:
					out.Write(b)
				case b := <-stderr:
					errout.Write(b)
				default:
					break drain
				}
			}
			return types.SuccessResponse(&RunCodeResponse{Stdout: out.String(), Stderr: errout.String()})
		}
	}
}
