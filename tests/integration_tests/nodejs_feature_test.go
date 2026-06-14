package integrationtests_test

import (
	"context"
	"strings"
	"testing"

	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/service"
)

func TestNodejsBasicTemplate(t *testing.T) {
	const code = `// declare main function
function main({a}) {
	return {b: a}
}

// decode and prepare input object
var inputs_obj = JSON.parse(Buffer.from('eyJhIjoiYSJ9', 'base64').toString('utf-8'))

// execute main function
var output_obj = main(inputs_obj)

// convert output to json and print
var output_json = JSON.stringify(output_obj)
var result = ` + "`<<RESULT>>${output_json}<<RESULT>>`" + `
console.log(result)`

	runMultipleTestings(t, 30, func(t *testing.T) {
		resp := service.RunNodeJsCode(context.TODO(), code, "", &types.RunnerOptions{
			EnableNetwork: true,
		})
		if resp.Code != 0 {
			t.Fatal(resp)
		}
	})
}

func TestNodejsBase64(t *testing.T) {
	// Test case for base64
	runMultipleTestings(t, 30, func(t *testing.T) {
		resp := service.RunNodeJsCode(context.TODO(), `
const base64 = Buffer.from("hello world").toString("base64");
console.log(Buffer.from(base64, "base64").toString());
		`, "", &types.RunnerOptions{
			EnableNetwork: true,
		})
		if resp.Code != 0 {
			t.Fatal(resp)
		}

		if resp.Data.(*service.RunCodeResponse).Stderr != "" {
			t.Fatalf("unexpected error: %s\n", resp.Data.(*service.RunCodeResponse).Stderr)
		}

		if !strings.Contains(resp.Data.(*service.RunCodeResponse).Stdout, "hello world") {
			t.Fatalf("unexpected output: %s\n", resp.Data.(*service.RunCodeResponse).Stdout)
		}
	})
}

func TestNodejsJSON(t *testing.T) {
	// Test case for json
	runMultipleTestings(t, 30, func(t *testing.T) {
		resp := service.RunNodeJsCode(context.TODO(), `
console.log(JSON.stringify({"hello": "world"}));
		`, "", &types.RunnerOptions{
			EnableNetwork: true,
		})
		if resp.Code != 0 {
			t.Error(resp)
		}

		if resp.Data.(*service.RunCodeResponse).Stderr != "" {
			t.Fatalf("unexpected error: %s\n", resp.Data.(*service.RunCodeResponse).Stderr)
		}

		if !strings.Contains(resp.Data.(*service.RunCodeResponse).Stdout, `{"hello":"world"}`) {
			t.Fatalf("unexpected output: %s\n", resp.Data.(*service.RunCodeResponse).Stdout)
		}
	})
}

func TestNodejsFd3Transport(t *testing.T) {
	resp := service.RunNodeJsCode(context.TODO(), `
const marker = "fd3-transport";
console.log(marker);
	`, "", &types.RunnerOptions{
		EnableNetwork: true,
	})
	if resp.Code != 0 {
		t.Fatal(resp)
	}

	if resp.Data.(*service.RunCodeResponse).Stderr != "" {
		t.Fatalf("unexpected error: %s\n", resp.Data.(*service.RunCodeResponse).Stderr)
	}

	if !strings.Contains(resp.Data.(*service.RunCodeResponse).Stdout, "fd3-transport") {
		t.Fatalf("unexpected output: %s\n", resp.Data.(*service.RunCodeResponse).Stdout)
	}
}

func TestNodejsConsoleErrorStaysInStderr(t *testing.T) {
	resp := service.RunNodeJsCode(context.TODO(), `
console.error("warn from stderr");
console.log("ok");
	`, "", &types.RunnerOptions{
		EnableNetwork: true,
	})
	if resp.Code != 0 {
		t.Fatal(resp)
	}

	data := resp.Data.(*service.RunCodeResponse)

	if !strings.Contains(data.Stdout, "ok") {
		t.Fatalf("unexpected output: %s\n", data.Stdout)
	}

	if !strings.Contains(data.Stderr, "warn from stderr") {
		t.Fatalf("expected console.error output in stderr, got: %s\n", data.Stderr)
	}

	if data.Error != "" {
		t.Fatalf("expected empty execution error, got: %s\n", data.Error)
	}

	if data.ExitCode != 0 {
		t.Fatalf("expected zero exit code, got: %d\n", data.ExitCode)
	}
}

func TestNodejsThrowPopulatesErrorAndStderr(t *testing.T) {
	resp := service.RunNodeJsCode(context.TODO(), `
throw new Error("bad input");
	`, "", &types.RunnerOptions{
		EnableNetwork: true,
	})
	if resp.Code != 0 {
		t.Fatal(resp)
	}

	data := resp.Data.(*service.RunCodeResponse)

	if !strings.Contains(data.Stderr, "Error: bad input") {
		t.Fatalf("expected exception in stderr, got: %s\n", data.Stderr)
	}

	if !strings.Contains(data.Error, "process exited with code") {
		t.Fatalf("expected error to include exit code, got: %s\n", data.Error)
	}

	if !strings.Contains(data.Error, data.Stderr) {
		t.Fatalf("expected error to include full stderr, got: %s\n", data.Error)
	}

	if data.ExitCode == 0 {
		t.Fatalf("expected non-zero exit code, got: %d\n", data.ExitCode)
	}
}
func TestNodejsNoProxyEnvPropagation(t *testing.T) {
	resp := service.RunNodeJsCode(context.TODO(), `
console.log(process.env.NO_PROXY || '');
	`, "", &types.RunnerOptions{
		EnableNetwork: true,
	})
	if resp.Code != 0 {
		t.Fatal(resp)
	}

	data := resp.Data.(*service.RunCodeResponse)
	if data.Stderr != "" {
		t.Fatalf("unexpected stderr: %s\n", data.Stderr)
	}
	if !strings.Contains(data.Stdout, "test.no-proxy.internal") {
		t.Fatalf("expected NO_PROXY to be propagated to subprocess, got: %q\n", data.Stdout)
	}
}

func TestNodejsAllowedEnvVarsPropagation(t *testing.T) {
	t.Setenv("TEST_SANDBOX_ENV_VAR", "hello_from_allowed_env")

	resp := service.RunNodeJsCode(context.TODO(), `
console.log(process.env.TEST_SANDBOX_ENV_VAR || '');
	`, "", &types.RunnerOptions{
		EnableNetwork: true,
	})
	if resp.Code != 0 {
		t.Fatal(resp)
	}

	data := resp.Data.(*service.RunCodeResponse)
	if data.Stderr != "" {
		t.Fatalf("unexpected stderr: %s\n", data.Stderr)
	}
	if !strings.Contains(data.Stdout, "hello_from_allowed_env") {
		t.Fatalf("expected TEST_SANDBOX_ENV_VAR to be propagated to subprocess, got: %q\n", data.Stdout)
	}
}

func TestNodejsUnlistedEnvVarNotPropagated(t *testing.T) {
	t.Setenv("UNLISTED_ENV_VAR", "should_not_appear")

	resp := service.RunNodeJsCode(context.TODO(), `
console.log(process.env.UNLISTED_ENV_VAR || 'not_found');
	`, "", &types.RunnerOptions{
		EnableNetwork: true,
	})
	if resp.Code != 0 {
		t.Fatal(resp)
	}

	data := resp.Data.(*service.RunCodeResponse)
	if data.Stderr != "" {
		t.Fatalf("unexpected stderr: %s\n", data.Stderr)
	}
	if strings.Contains(data.Stdout, "should_not_appear") {
		t.Fatalf("expected UNLISTED_ENV_VAR NOT to be propagated, but it was: %q\n", data.Stdout)
	}
}
