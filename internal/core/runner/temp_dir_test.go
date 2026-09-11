package runner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareTempDirDoesNotChangeProcessWorkingDirectory(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}

	runner := TempDirRunner{}
	tmpDir, err := runner.PrepareTempDir(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("prepare temp dir failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	currentWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd after prepare failed: %v", err)
	}

	if currentWD != originalWD {
		t.Fatalf("expected working directory to remain %q, got %q", originalWD, currentWD)
	}
}

func TestPrepareTempDirCopiesRequiredPaths(t *testing.T) {
	baseDir := t.TempDir()
	sourceDir := filepath.Join(baseDir, "source")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "marker.txt"), []byte("ok"), 0644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	runner := TempDirRunner{}
	tmpDir, err := runner.PrepareTempDir(baseDir, []string{sourceDir})
	if err != nil {
		t.Fatalf("prepare temp dir failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	markerPath := filepath.Join(tmpDir, sourceDir, "marker.txt")
	data, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read copied marker: %v", err)
	}
	if string(data) != "ok" {
		t.Fatalf("unexpected marker content: %q", string(data))
	}
}
