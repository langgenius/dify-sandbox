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

func TestIsolatedLaunchPreservesNativeSeccompAndPrivateCodePipe(t *testing.T) {
	launch := IsolatedLaunch("/usr/local/bin/python3")
	if launch.Executable != "/usr/local/bin/python3" || launch.BootstrapPath != "/work/bootstrap" {
		t.Fatal("invalid native launch paths")
	}
	if !strings.Contains(launch.Bootstrap, "lib.DifySeccompIsolated(") || !strings.Contains(launch.Bootstrap, "os.fdopen(3") {
		t.Fatal("native bootstrap isolation removed")
	}
	if strings.Contains(launch.Bootstrap, `os.chdir("/work")`) {
		t.Fatal("cwd must be set before seccomp inside the native entry point")
	}
}
