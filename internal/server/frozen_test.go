package server

import (
	"github.com/langgenius/dify-sandbox/internal/static"
	"testing"
)

func TestBuilderDoesNotStartDependencyOwner(t *testing.T) {
	t.Setenv("SANDBOX_PROFILE", "builder")
	initDependencies()
	if err := updatePythonDependencies(static.RunnerDependencies{}); err == nil {
		t.Fatal("builder update owner allowed mutation")
	}
}
