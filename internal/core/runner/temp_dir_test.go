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

func TestTempDirRecordsOwnershipBeforeCreation(t *testing.T) {
	base := t.TempDir()
	if err := os.Mkdir(filepath.Join(base, "tmp"), 0700); err != nil {
		t.Fatal(err)
	}
	r := TempDirRunner{}
	called := false
	err := r.WithTempDirTracked(base, nil, func(root string) error {
		called = true
		if _, err := os.Lstat(root); !os.IsNotExist(err) {
			t.Fatal("resource created before ownership was recorded")
		}
		return os.ErrPermission
	}, func(string) error { t.Fatal("dispatch followed journal failure"); return nil })
	if !called || err != os.ErrPermission {
		t.Fatalf("ownership failure ignored: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(base, "tmp"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("untracked resources leaked: %v %v", entries, err)
	}
}
