package integrationtests_test

import (
	"io"
	"testing"

	"github.com/langgenius/dify-sandbox/internal/service"
	"github.com/langgenius/dify-sandbox/internal/storage"
)

func TestFileDownload(t *testing.T) {
	// 1. Run code that generates a file
	resp := service.RunPython3Code(`
with open("output.txt", "w") as f:
    f.write("Hello, World!")
	`, "", false, nil, []string{"output.txt"})

	if resp.Code != 0 {
		t.Fatalf("Run failed with code %d. Message: %s", resp.Code, resp.Message)
	}

	data := resp.Data.(*service.RunCodeResponse)
	if data.Stderr != "" {
		t.Fatalf("unexpected content error. Stdout: %s, Stderr: %s", data.Stdout, data.Stderr)
	}

	// 2. Add assertions for files
	if len(data.Files) == 0 {
		t.Fatalf("Expected files in response, got none")
	}

	fileId, ok := data.Files["output.txt"]
	if !ok {
		t.Fatalf("Expected output.txt in files, got %v", data.Files)
	}

	// 3. Retrieve content from storage
	store := storage.GetStorage()
	reader, err := store.Get(fileId)
	if err != nil {
		t.Fatalf("Failed to retrieve file from storage: %v", err)
	}
	defer reader.Close()

	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("Failed to read file content: %v", err)
	}

	if string(content) != "Hello, World!" {
		t.Fatalf("Unexpected content: '%s'", string(content))
	}
}
