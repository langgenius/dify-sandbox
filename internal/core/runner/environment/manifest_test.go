package environment

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func writeTestEnvironment(t *testing.T) (string, EnvironmentManifest) {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"usr/local/lib/python3.12/site-packages", "opt/node/node_modules", "lib", "work", "provenance"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"usr/local/lib/python3.12/library", "opt/node/node_modules/library", "lib/library", "provenance/licenses.json"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("original"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	m, err := BuildManifest(root, EnvironmentManifest{SchemaVersion: 1, PythonVersion: "3.12.12", NodeVersion: "20.11.1", JinjaVersion: "3.1.6", Architecture: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	if err = WriteManifest(root, m); err != nil {
		t.Fatal(err)
	}
	return root, m
}
func TestManifestDetectsMutation(t *testing.T) {
	for _, name := range []string{"usr/local/lib/python3.12/library", "opt/node/node_modules/library", "lib/library", "provenance/licenses.json"} {
		t.Run(name, func(t *testing.T) {
			root, m := writeTestEnvironment(t)
			if err := VerifyEnvironment(root, m); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, name), []byte("changed"), 0644); err != nil {
				t.Fatal(err)
			}
			if VerifyEnvironment(root, m) == nil {
				t.Fatal("mutable environment accepted")
			}
		})
	}
}
func TestManifestDetectsPathModeAndLinkDrift(t *testing.T) {
	for _, mutate := range []func(string) error{
		func(r string) error {
			return os.Rename(filepath.Join(r, "lib/library"), filepath.Join(r, "lib/renamed"))
		},
		func(r string) error { return os.Chmod(filepath.Join(r, "lib/library"), 0777) },
		func(r string) error { return os.Symlink("/etc/passwd", filepath.Join(r, "lib/injected")) },
		func(r string) error { return os.WriteFile(filepath.Join(r, "extra"), nil, 0644) },
		func(r string) error { return os.Remove(filepath.Join(r, "lib/library")) },
	} {
		root, m := writeTestEnvironment(t)
		if err := mutate(root); err != nil {
			t.Fatal(err)
		}
		if VerifyEnvironment(root, m) == nil {
			t.Fatal("tree drift accepted")
		}
	}
}
func TestManifestRejectsEscapeAndSpecialFiles(t *testing.T) {
	root, _ := writeTestEnvironment(t)
	if err := os.Symlink("../../outside", filepath.Join(root, "lib/escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildManifest(root, EnvironmentManifest{}); err == nil {
		t.Fatal("escape accepted")
	}
}
func TestManifestCannotRedefineExpectedArtifact(t *testing.T) {
	root, m := writeTestEnvironment(t)
	bad := m
	bad.PythonVersion = "changed"
	if VerifyEnvironment(root, bad) == nil {
		t.Fatal("metadata drift accepted")
	}
	if err := os.Chmod(filepath.Join(root, ManifestFile), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ManifestFile), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAndVerify(root, m.Digest); err == nil {
		t.Fatal("manifest replacement accepted")
	}
}
func TestManifestRejectsSymlinkManifestAndRoot(t *testing.T) {
	root, m := writeTestEnvironment(t)
	p := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, p); err != nil {
		t.Fatal(err)
	}
	if VerifyEnvironment(p, m) == nil {
		t.Fatal("symlink root accepted")
	}
	if err := os.Rename(filepath.Join(root, ManifestFile), filepath.Join(t.TempDir(), "saved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("lib/library", filepath.Join(root, ManifestFile)); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAndVerify(root, m.Digest); err == nil {
		t.Fatal("symlink manifest accepted")
	}
}
func TestManifestDetectsSymlinkRetarget(t *testing.T) {
	root, m := writeTestEnvironment(t)
	link := filepath.Join(root, "link")
	if err := os.Symlink("/lib/library", link); err != nil {
		t.Fatal(err)
	}
	m, err := BuildManifest(root, m)
	if err != nil {
		t.Fatal(err)
	}
	if err = VerifyEnvironment(root, m); err != nil {
		t.Fatal(err)
	}
	os.Remove(link)
	if err = os.Symlink("/opt/node/node_modules/library", link); err != nil {
		t.Fatal(err)
	}
	if VerifyEnvironment(root, m) == nil {
		t.Fatal("symlink target drift accepted")
	}
}
func TestManifestRejectsSpecialFile(t *testing.T) {
	root, m := writeTestEnvironment(t)
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	if VerifyEnvironment(root, m) == nil {
		t.Fatal("FIFO accepted")
	}
}
