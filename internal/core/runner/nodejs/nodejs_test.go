package nodejs

import (
	"strings"
	"testing"

	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
)

func TestBuildBootstrapInjectsPreloadButNotUserCode(t *testing.T) {
	bootstrap := buildBootstrap("globalThis.preloaded = true;")

	if !strings.Contains(bootstrap, "globalThis.preloaded = true;") {
		t.Fatal("expected preload in bootstrap")
	}

	if !strings.Contains(bootstrap, "readFileSync(3, 'utf8')") {
		t.Fatal("expected bootstrap to read code from fd 3")
	}

	if strings.Contains(bootstrap, "console.log('user code')") {
		t.Fatal("bootstrap unexpectedly contains user code")
	}
}

func TestBuildCommandArgsUsesProvidedUID(t *testing.T) {
	args := buildCommandArgs("/tmp/test.js", 10042, &types.RunnerOptions{})

	if args[0] != "--jitless" {
		t.Fatalf("expected --jitless first, got %q", args[0])
	}

	if args[1] != "/tmp/test.js" {
		t.Fatalf("expected script path second, got %q", args[1])
	}

	if args[2] != "10042" {
		t.Fatalf("expected provided uid, got %q", args[2])
	}
}
