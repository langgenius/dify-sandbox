package integrationtests_test

import (
	"strings"
	"testing"
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/service"
)

func TestUvBase64(t *testing.T) {
	// Test case for base64
	runMultipleTestings(t, 50, func(t *testing.T) {
		resp := service.RunUvCode(`
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

func TestUvJSON(t *testing.T) {
	runMultipleTestings(t, 50, func(t *testing.T) {
		// Test case for json
		resp := service.RunUvCode(`
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

func TestUvRequests(t *testing.T) {
	// Test case for http
	runMultipleTestings(t, 1, func(t *testing.T) {
		resp := service.RunUvCode(`
import requests
print(requests.get("https://www.bilibili.com").content)
	`, "", &types.RunnerOptions{
			EnableNetwork: true,
			Dependenchies: []types.Dependency{
				{
					Name:    "requests",
					Version: "2.32.3",
				},
			},
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

func TestUvHttpx(t *testing.T) {
	// Test case for http
	runMultipleTestings(t, 1, func(t *testing.T) {
		resp := service.RunUvCode(`
import httpx
print(httpx.get("https://www.bilibili.com").content)
	`, "", &types.RunnerOptions{
			EnableNetwork: true,
			Dependenchies: []types.Dependency{
				{
					Name: "httpx",
				},
			},
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

func TestUvTimezone(t *testing.T) {
	// Test case for time
	runMultipleTestings(t, 1, func(t *testing.T) {
		resp := service.RunUvCode(`
from datetime import datetime
from zoneinfo import ZoneInfo

print(datetime.now(ZoneInfo("Asia/Shanghai")).isoformat())
		`, "", &types.RunnerOptions{
			EnableNetwork: true,
		})
		if resp.Code != 0 {
			t.Fatal(resp)
		}

		if resp.Data.(*service.RunCodeResponse).Stderr != "" {
			t.Fatalf("unexpected error: %s\n", resp.Data.(*service.RunCodeResponse).Stderr)
		}

		stdout := resp.Data.(*service.RunCodeResponse).Stdout
		// trim \n
		stdout = strings.TrimSpace(stdout)
		// check if stdout match time format
		_, err := time.Parse("2006-01-02T15:04:05.000000+08:00", stdout)
		if err != nil {
			t.Fatalf("unexpected output: %s, error: %v\n", stdout, err)
		}
	})
}
