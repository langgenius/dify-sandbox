//go:build linux

package python

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
)

func TestInitializeEnvironmentKeepsSuccessfulWrite(t *testing.T) {
	p := PythonRunner{}
	options := &types.RunnerOptions{}
	bootstrapPath, err := p.InitializeEnvironment("# preload", options, os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(bootstrapPath) })
	content, err := os.ReadFile(bootstrapPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != buildBootstrap("# preload", options, os.Getuid()) {
		t.Fatal("successful bootstrap contents changed")
	}
}

func TestInitializeEnvironmentCleansPartialWrite(t *testing.T) {
	const helperEnv = "DIFY_TEST_PARTIAL_BOOTSTRAP_WRITE"
	if os.Getenv(helperEnv) != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestInitializeEnvironmentCleansPartialWrite$")
		cmd.Env = append(os.Environ(), helperEnv+"=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("partial-write subprocess failed: %v\n%s", err, output)
		}
		return
	}

	// Limit only this subprocess, without filling the disk or affecting other tests.
	var original syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &original); err != nil {
		t.Fatal(err)
	}
	signal.Ignore(syscall.SIGXFSZ)
	limited := original
	limited.Cur = 1024
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limited); err != nil {
		t.Fatal(err)
	}
	defer syscall.Setrlimit(syscall.RLIMIT_FSIZE, &original)

	p := PythonRunner{}
	bootstrapPath, err := p.InitializeEnvironment(strings.Repeat("# preload\n", 1024), &types.RunnerOptions{}, os.Getuid())
	if !errors.Is(err, syscall.EFBIG) {
		t.Fatalf("expected file-size-limit error, got path=%q err=%v", bootstrapPath, err)
	}
	if bootstrapPath != "" {
		t.Fatalf("expected no usable bootstrap path, got %q", bootstrapPath)
	}
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("expected original filesystem error, got %T", err)
	}
	if filepath.Dir(pathErr.Path) != filepath.Join(LIB_PATH, "tmp") {
		t.Fatalf("unexpected failed path: %q", pathErr.Path)
	}
	t.Cleanup(func() { _ = os.Remove(pathErr.Path) })
	if _, statErr := os.Stat(pathErr.Path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("partial bootstrap file was not removed: %v", statErr)
	}
}
