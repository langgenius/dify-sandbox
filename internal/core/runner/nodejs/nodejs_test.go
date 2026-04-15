package nodejs

import (
	"strings"
	"testing"
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
