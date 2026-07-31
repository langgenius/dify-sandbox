package python

import (
	"strings"
	"testing"

	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
)

func TestBuildBootstrapInjectsPreloadButNotUserCode(t *testing.T) {
	bootstrap := buildBootstrap("print('preload')", &types.RunnerOptions{EnableNetwork: true}, 123)

	if !strings.Contains(bootstrap, "print('preload')") {
		t.Fatal("expected preload in bootstrap")
	}

	if !strings.Contains(bootstrap, "os.fdopen(3") {
		t.Fatal("expected bootstrap to read code from fd 3")
	}

	if strings.Contains(bootstrap, "print('user code')") {
		t.Fatal("bootstrap unexpectedly contains user code")
	}

	if strings.Contains(bootstrap, "{{enable_network}}") {
		t.Fatal("bootstrap contains an unresolved network placeholder")
	}

	if !strings.Contains(bootstrap, "_preload_supported_runtime_modules(bool(1))") {
		t.Fatal("expected network runtime modules to be preloaded")
	}

	networkDisabled := buildBootstrap("", &types.RunnerOptions{}, 123)
	if !strings.Contains(networkDisabled, "_preload_supported_runtime_modules(bool(0))") {
		t.Fatal("expected network runtime module preload to be disabled")
	}
}
