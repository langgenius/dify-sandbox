package nodejs

import (
	"strconv"
	"strings"
	"testing"

	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/static"
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

	if args[0] != "/tmp/test.js" {
		t.Fatalf("expected script path first, got %q", args[0])
	}

	if args[1] != "10042" {
		t.Fatalf("expected provided uid, got %q", args[1])
	}

	if args[1] == strconv.Itoa(static.SANDBOX_USER_UID) {
		t.Fatal("expected command args to avoid shared sandbox uid")
	}
}
