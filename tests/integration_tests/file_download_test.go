package integrationtests_test

import (
	"testing"
	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/service"
)

func TestFileDownload(t *testing.T) {
	code := `
with open('output.txt', 'w') as f:
    f.write('Hello, World!')
`
	resp := service.RunPython3Code(code, "", &types.RunnerOptions{
		EnableNetwork: false,
		FetchFiles:    []string{"output.txt"},
	})

	if resp.Code != 0 {
		t.Fatalf("Run failed: %d, %s", resp.Code, resp.Message)
	}

	respData, ok := resp.Data.(*service.RunCodeResponse)
	if !ok {
		t.Fatalf("Invalid response data type")
	}

	if respData.Files == nil {
		t.Fatalf("Files map is nil")
	}

	// ...
	content, ok := respData.Files["output.txt"]
	if !ok {
		t.Fatalf("output.txt not returned in files. Stderr: %s", respData.Stderr)
	}

	if string(content) != "Hello, World!" {
		t.Errorf("Unexpected content: '%s'. Stderr: %s. Stdout: %s", string(content), respData.Stderr, respData.Stdout)
	}
}
