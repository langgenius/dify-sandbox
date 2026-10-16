package uidpool

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPersistentPoolRejectsWritableNonstickyAncestors(t *testing.T) {
	for _, mode := range []os.FileMode{0770, 0707, 0777} {
		t.Run(mode.String(), func(t *testing.T) {
			ancestor := filepath.Join(t.TempDir(), "ancestor")
			if err := os.Mkdir(ancestor, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(ancestor, mode); err != nil {
				t.Fatal(err)
			}
			if _, err := NewPersistentUIDPool(21000, 21002, filepath.Join(ancestor, "leases")); err == nil {
				t.Fatal("writable nonsticky ancestor accepted")
			}
		})
	}
}

func TestPersistentPoolPrivateRootStillRequiredUnderStickyAncestor(t *testing.T) {
	ancestor := filepath.Join(t.TempDir(), "ancestor")
	if err := os.Mkdir(ancestor, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ancestor, os.ModeSticky|0777); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(ancestor, "leases")
	if _, err := NewPersistentUIDPool(21000, 21002, root); err != nil {
		t.Fatal("trusted sticky ancestor rejected", err)
	}
	if err := os.Chmod(root, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPersistentUIDPool(21000, 21002, root); err == nil {
		t.Fatal("public lease root accepted under sticky ancestor")
	}
}
