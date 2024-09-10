package integrationtests_test

import (
	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/service"
	"strings"
	"testing"
)

func TestPyModuleAutoImport(t *testing.T) {
	code :=
		` import pandas
          print(pandas.__version__)
        `
	resp := service.RunPython3Code(code, "", &types.RunnerOptions{
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
}
