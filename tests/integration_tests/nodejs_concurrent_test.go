package integrationtests_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/service"
)

func TestNodejsConcurrentSimpleExecution(t *testing.T) {
	const code = `console.log(123)`

	runConcurrentTestings(t, 16, 8, func(iteration int) error {
		resp := service.RunNodeJsCode(context.TODO(), code, "", &types.RunnerOptions{
			EnableNetwork: true,
		})
		if resp.Code != 0 {
			return fmt.Errorf("iteration %d: request failed: %+v", iteration, resp)
		}

		data := resp.Data.(*service.RunCodeResponse)
		if strings.Contains(data.Error, "signal: killed") {
			return fmt.Errorf("iteration %d: process was killed: %s", iteration, data.Error)
		}
		if data.Error != "" {
			return fmt.Errorf("iteration %d: unexpected execution error: %s", iteration, data.Error)
		}
		if data.ExitCode != 0 {
			return fmt.Errorf(
				"iteration %d: expected zero exit code, got %d (error=%q stderr=%q)",
				iteration, data.ExitCode, data.Error, data.Stderr,
			)
		}
		if !strings.Contains(data.Stdout, "123") {
			return fmt.Errorf("iteration %d: unexpected stdout: %q", iteration, data.Stdout)
		}

		return nil
	})
}
