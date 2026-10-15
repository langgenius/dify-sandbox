package runner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTempDirDoesNotMutateProcessWorkingDirectory(t *testing.T) {
	before, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	if err = os.Mkdir(filepath.Join(base, "tmp"), 0700); err != nil {
		t.Fatal(err)
	}
	r := TempDirRunner{}
	err = r.WithTempDir(base, nil, func(root string) error {
		defer os.RemoveAll(root)
		during, _ := os.Getwd()
		if during != before {
			t.Error("process cwd changed during request")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.Getwd()
	if after != before {
		t.Fatal("process cwd changed after request")
	}
}
