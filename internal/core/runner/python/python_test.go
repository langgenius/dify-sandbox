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
}
