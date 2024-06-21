package integrationtests_test

import (
	"strings"
	"testing"

	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/service"
)

func TestNodejsBase64(t *testing.T) {
	// Test case for base64
	resp := service.RunNodeJsCode(`
const base64 = Buffer.from("hello world").toString("base64");
console.log(Buffer.from(base64, "base64").toString());
	`, "", &types.RunnerOptions{
		EnableNetwork: true,
	})
	if resp.Code != 0 {
		t.Error(resp)
	}

	if !strings.Contains(resp.Data.(*service.RunCodeResponse).Stdout, "hello world") {
		t.Errorf("unexpected output: %s\n", resp.Data.(*service.RunCodeResponse).Stdout)
	}

	if resp.Data.(*service.RunCodeResponse).Stderr != "" {
		t.Errorf("unexpected error: %s\n", resp.Data.(*service.RunCodeResponse).Stderr)
	}
}

func TestNodejsJSON(t *testing.T) {
	// Test case for json
	resp := service.RunNodeJsCode(`
console.log(JSON.stringify({"hello": "world"}));
	`, "", &types.RunnerOptions{
		EnableNetwork: true,
	})
	if resp.Code != 0 {
		t.Error(resp)
	}

	if !strings.Contains(resp.Data.(*service.RunCodeResponse).Stdout, `{"hello":"world"}`) {
		t.Errorf("unexpected output: %s\n", resp.Data.(*service.RunCodeResponse).Stdout)
	}

	if resp.Data.(*service.RunCodeResponse).Stderr != "" {
		t.Errorf("unexpected error: %s\n", resp.Data.(*service.RunCodeResponse).Stderr)
	}
}
