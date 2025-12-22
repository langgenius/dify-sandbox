package integrationtests_test

import (
	"strings"
	"testing"

	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/service"
)

func TestFileUpload(t *testing.T) {
	// Test case for file upload
	resp := service.RunPython3Code(`
print(open("test.txt").read())
	`, "", &types.RunnerOptions{
		EnableNetwork: false,
		Files: map[string]string{
			"test.txt": "hello verification",
		},
	})

	if resp.Code != 0 {
		t.Fatalf("Run failed with code %d. Stdout: %s, Stderr: %s", resp.Code, resp.Data.(*service.RunCodeResponse).Stdout, resp.Data.(*service.RunCodeResponse).Stderr)
	}

	if resp.Data.(*service.RunCodeResponse).Stderr != "" {
		t.Fatalf("unexpected error: %s. Stdout: %s", resp.Data.(*service.RunCodeResponse).Stderr, resp.Data.(*service.RunCodeResponse).Stdout)
	}

	if !strings.Contains(resp.Data.(*service.RunCodeResponse).Stdout, "hello verification") {
		t.Fatalf("unexpected output: %s\n", resp.Data.(*service.RunCodeResponse).Stdout)
	}
}
