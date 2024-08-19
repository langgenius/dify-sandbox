package integrationtests_test

import (
	"strings"
	"testing"

	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/service"
)

func TestPythonBase64(t *testing.T) {
	// Test case for base64
	runMultipleTestings(t, 50, func(t *testing.T) {
		resp := service.RunPython3Code(`
import base64
print(base64.b64decode(base64.b64encode(b"hello world")).decode())
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

func TestPythonJSON(t *testing.T) {
	runMultipleTestings(t, 50, func(t *testing.T) {
		// Test case for json
		resp := service.RunPython3Code(`
import json
print(json.dumps({"hello": "world"}))
		`, "", &types.RunnerOptions{
			EnableNetwork: true,
		})
		if resp.Code != 0 {
			t.Fatal(resp)
		}

		if resp.Data.(*service.RunCodeResponse).Stderr != "" {
			t.Fatalf("unexpected error: %s\n", resp.Data.(*service.RunCodeResponse).Stderr)
		}

		if !strings.Contains(resp.Data.(*service.RunCodeResponse).Stdout, `{"hello": "world"}`) {
			t.Fatalf("unexpected output: %s\n", resp.Data.(*service.RunCodeResponse).Stdout)
		}
	})
}

func TestPythonHttp(t *testing.T) {
	// Test case for http
	runMultipleTestings(t, 1, func(t *testing.T) {
		resp := service.RunPython3Code(`
import requests
print(requests.get("https://www.bilibili.com").content)
	`, "", &types.RunnerOptions{
			EnableNetwork: true,
		})
		if resp.Code != 0 {
			t.Fatal(resp)
		}

		if resp.Data.(*service.RunCodeResponse).Stderr != "" {
			t.Fatalf("unexpected error: %s\n", resp.Data.(*service.RunCodeResponse).Stderr)
		}

		if !strings.Contains(resp.Data.(*service.RunCodeResponse).Stdout, "bilibili") {
			t.Fatalf("unexpected output: %s\n", resp.Data.(*service.RunCodeResponse).Stdout)
		}
	})
}
