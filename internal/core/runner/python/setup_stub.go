//go:build !linux

package python

import (
	"fmt"

	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/utils/log"
)

const (
	LIB_PATH = "/var/sandbox/sandbox-python"
	LIB_NAME = "python.so"
)

func init() {
	log.Warn("Python native runner is only supported on Linux. Please use microsandbox backend on this platform.")
}

func releaseLibBinary(force_remove_old_lib bool) {
	log.Warn("Cannot release Python lib binary on non-Linux platform")
}

func checkLibAvaliable() bool {
	return false
}

func InstallDependencies(requirements string) error {
	log.Warn("Cannot install Python dependencies on non-Linux platform")
	return nil
}

func ListDependencies() []types.Dependency {
	log.Warn("Cannot list Python dependencies on non-Linux platform")
	return []types.Dependency{}
}

func RefreshDependencies() []types.Dependency {
	log.Warn("Cannot refresh Python dependencies on non-Linux platform")
	return []types.Dependency{}
}

func PreparePythonDependenciesEnv() error {
	log.Warn("Cannot prepare Python dependencies environment on non-Linux platform")
	return fmt.Errorf("Python native runner is only supported on Linux. Please use microsandbox backend")
}
