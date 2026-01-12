package integrationtests_test

import (
	"strings"
	"testing"

	"github.com/langgenius/dify-sandbox/internal/service"
	"github.com/langgenius/dify-sandbox/internal/storage"
)

func TestFileUpload(t *testing.T) {
	// 1. Upload file to storage
	store := storage.GetStorage()
	content := "hello verification"
	reader := strings.NewReader(content)
	fileId, err := store.Put(reader, "test.txt")
	if err != nil {
		t.Fatalf("failed to put file: %v", err)
	}

	// 2. Run code with file_id
	resp := service.RunPython3Code(`
print(open("test.txt").read())
	`, "", false, map[string]string{
		"test.txt": fileId,
	}, nil)

	if resp.Code != 0 {
		t.Fatalf("Run failed with code %d. Message: %s", resp.Code, resp.Message)
	}

	if resp.Data.(*service.RunCodeResponse).Stderr != "" {
		t.Fatalf("unexpected error: %s. Stdout: %s", resp.Data.(*service.RunCodeResponse).Stderr, resp.Data.(*service.RunCodeResponse).Stdout)
	}

	if !strings.Contains(resp.Data.(*service.RunCodeResponse).Stdout, "hello verification") {
		t.Fatalf("unexpected output: %s\n", resp.Data.(*service.RunCodeResponse).Stdout)
	}
}
