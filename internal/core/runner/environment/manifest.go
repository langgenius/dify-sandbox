// Package environment binds a trusted profile to a complete, baked runtime tree.
// Manifest generation is a build-time operation; requests never choose roots.
package environment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
)

const ManifestFile = "environment-manifest.json"

type EnvironmentManifest struct {
	SchemaVersion                                         int
	Digest, PythonVersion, NodeVersion, JinjaVersion      string
	PythonTreeDigest, NodeTreeDigest, NativeLibraryDigest string
	Architecture, TreeDigest                              string
	Files                                                 int64
	Bytes                                                 int64
}

type entry struct {
	Path     string
	Mode     uint32
	UID, GID uint32
	Size     int64
	Content  string
}

func hashJSON(v any) string {
	b, _ := json.Marshal(v)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// BuildManifest hashes every path, file byte, permission, owner and link target.
// Category digests are informational; TreeDigest also covers the interpreter,
// certificates, licenses and every other file, including empty directories.
func BuildManifest(root string, metadata EnvironmentManifest) (EnvironmentManifest, error) {
	return buildManifest(context.Background(), root, metadata)
}

func buildManifest(ctx context.Context, root string, metadata EnvironmentManifest) (EnvironmentManifest, error) {
	if err := ctx.Err(); err != nil {
		return metadata, err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return metadata, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return metadata, fmt.Errorf("environment root must be a directory, not a link")
	}
	var all, python, node, native []entry
	var total int64
	err = filepath.WalkDir(root, func(name string, d os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == ManifestFile {
			return nil
		}
		info, err := os.Lstat(name)
		if err != nil {
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("Linux metadata required")
		}
		e := entry{Path: rel, Mode: uint32(info.Mode()), UID: st.Uid, GID: st.Gid}
		switch {
		case info.Mode().IsRegular():
			// O_NOFOLLOW prevents a link substitution between Lstat and open.
			fd, err := syscall.Open(name, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
			if err != nil {
				return err
			}
			f := os.NewFile(uintptr(fd), name)
			opened, err := f.Stat()
			if err != nil {
				f.Close()
				return err
			}
			if !os.SameFile(info, opened) {
				f.Close()
				return fmt.Errorf("environment changed during verification")
			}
			h := sha256.New()
			n, err := io.Copy(h, contextReader{ctx: ctx, reader: f})
			after, statErr := f.Stat()
			f.Close()
			if err != nil {
				return err
			}
			if statErr != nil {
				return statErr
			}
			if n != info.Size() || !info.ModTime().Equal(after.ModTime()) || info.Size() != after.Size() {
				return fmt.Errorf("environment changed during verification")
			}
			e.Size = n
			e.Content = hex.EncodeToString(h.Sum(nil))
			total += n
		case info.IsDir():
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(name)
			if err != nil {
				return err
			}
			e.Content = target
			// Absolute links are rooted at the eventual chroot, never the service root.
			resolved := path.Clean(path.Join(path.Dir(rel), target))
			if path.IsAbs(target) {
				resolved = path.Clean(strings.TrimPrefix(target, "/"))
			}
			if resolved == ".." || strings.HasPrefix(resolved, "../") {
				return fmt.Errorf("environment symlink escapes root: %s", rel)
			}
			if err := validateLink(ctx, root, rel, 0); err != nil {
				return err
			}
		default:
			return fmt.Errorf("special file in environment: %s", rel)
		}
		all = append(all, e)
		if strings.HasPrefix(rel, "usr/local/lib/python") {
			python = append(python, e)
		}
		if strings.HasPrefix(rel, "opt/node/") || rel == "usr/bin/node" {
			node = append(node, e)
		}
		if strings.HasPrefix(rel, "lib/") || strings.HasPrefix(rel, "lib64/") || strings.Contains(rel, ".so") || strings.HasSuffix(rel, ".node") {
			native = append(native, e)
		}
		return nil
	})
	if err != nil {
		return metadata, err
	}
	if err := ctx.Err(); err != nil {
		return metadata, err
	}
	metadata.SchemaVersion = 1
	metadata.TreeDigest = hashJSON(all)
	metadata.PythonTreeDigest = hashJSON(python)
	metadata.NodeTreeDigest = hashJSON(node)
	metadata.NativeLibraryDigest = hashJSON(native)
	metadata.Files = int64(len(all))
	metadata.Bytes = total
	metadata.Digest = ""
	metadata.Digest = hashJSON(metadata)
	return metadata, nil
}

// Resolve links component-by-component using chroot semantics. EvalSymlinks on
// the host would incorrectly follow absolute links into the service filesystem.
func validateLink(ctx context.Context, root, rel string, depth int) error {
	if depth > 40 {
		return fmt.Errorf("symlink loop")
	}
	parts := strings.Split(rel, "/")
	current := ""
	for i, p := range parts {
		if err := ctx.Err(); err != nil {
			return err
		}
		current = path.Join(current, p)
		info, err := os.Lstat(filepath.Join(root, current))
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(filepath.Join(root, current))
			if err != nil {
				return err
			}
			next := path.Join(path.Dir(current), target)
			if path.IsAbs(target) {
				next = strings.TrimPrefix(target, "/")
			}
			next = path.Clean(next)
			if next == ".." || strings.HasPrefix(next, "../") {
				return fmt.Errorf("symlink escape")
			}
			if i+1 < len(parts) {
				next = path.Join(next, path.Join(parts[i+1:]...))
			}
			return validateLink(ctx, root, next, depth+1)
		}
	}
	return nil
}

// Checkpoints bound cooperative hashing work, not a kernel read already in
// progress. The supervisor separately bounds waiting and retains ownership if
// an operation cannot stop; it must never assume cancellation killed this read.
type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
func WriteManifest(root string, m EnvironmentManifest) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, ManifestFile), append(b, '\n'), 0444)
}
