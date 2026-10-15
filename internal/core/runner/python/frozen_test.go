package python

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuilderDependencyMutationRefused(t *testing.T) {
	t.Setenv("SANDBOX_PROFILE", "builder")
	marker := filepath.Join(t.TempDir(), "pip-ran")
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "pip3"), []byte("#!/bin/sh\n: > "+marker+"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if err := InstallDependencies("requests==2.32.3"); err == nil {
		t.Fatal("builder install allowed")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("pip invoked")
	}
	if err := PreparePythonDependenciesEnv(); err == nil {
		t.Fatal("builder dependency preparation allowed")
	}
}
