package environment

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
)

func VerifyEnvironment(root string, expected EnvironmentManifest) error {
	return verifyEnvironment(context.Background(), root, expected)
}
func verifyEnvironment(ctx context.Context, root string, expected EnvironmentManifest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if expected.SchemaVersion != 1 || expected.PythonVersion == "" || expected.NodeVersion == "" || expected.JinjaVersion == "" || (expected.Architecture != "arm64" && expected.Architecture != "amd64") || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(expected.Digest) {
		return fmt.Errorf("invalid environment manifest")
	}
	digest := expected.Digest
	expected.Digest = ""
	if hashJSON(expected) != digest {
		return fmt.Errorf("manifest digest mismatch")
	}
	expected.Digest = digest
	actual, err := buildManifest(ctx, root, expected)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("environment tree differs from baked manifest")
	}
	return nil
}

func LoadAndVerify(root, digest string) (EnvironmentManifest, error) {
	return LoadAndVerifyContext(context.Background(), root, digest)
}

// LoadAndVerifyContext cooperatively stops between filesystem operations. It
// cannot interrupt a blocked syscall; callers must retain its resource owner.
func LoadAndVerifyContext(ctx context.Context, root, digest string) (EnvironmentManifest, error) {
	var m EnvironmentManifest
	if err := ctx.Err(); err != nil {
		return m, err
	}
	fd, err := syscall.Open(filepath.Join(root, ManifestFile), syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if err != nil {
		return m, err
	}
	f := os.NewFile(uintptr(fd), ManifestFile)
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return m, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0222 != 0 || info.Size() > 16384 {
		return m, fmt.Errorf("invalid manifest file")
	}
	dec := json.NewDecoder(io.LimitReader(contextReader{ctx: ctx, reader: f}, 16385))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&m); err != nil {
		return m, err
	}
	if dec.Decode(new(any)) != io.EOF {
		return m, fmt.Errorf("trailing manifest content")
	}
	if m.Digest != digest {
		return m, fmt.Errorf("profile environment digest mismatch")
	}
	return m, verifyEnvironment(ctx, root, m)
}

// BuilderProfile is selected once by trusted deployment configuration. It is
// never populated from a request or inherited by the submitted-code process.
func BuilderProfile() bool { return os.Getenv("SANDBOX_PROFILE") == "builder" }
func RequireMutable() error {
	if BuilderProfile() {
		return fmt.Errorf("builder dependencies are immutable")
	}
	return nil
}
