package uidpool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// A separate server process must retain the lease even before it has launched
// any UID-owned process. Killing that server simulates restart, not Release.
func TestOrdinaryLeaseAcrossServerRestart(t *testing.T) {
	if os.Getenv("UID_LEASE_HELPER") == "1" {
		pool, err := NewPersistentUIDPool(10000, 10001, os.Getenv("UID_LEASE_ROOT"))
		if err != nil {
			t.Fatal(err)
		}
		uid, err := pool.Acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		resource := os.Getenv("UID_LEASE_RESOURCE")
		if err := pool.TrackResource(uid, resource); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(resource, []byte("retained execution resource"), 0600); err != nil {
			t.Fatal(err)
		}
		fmt.Println("ready")
		var b [1]byte
		os.Stdin.Read(b[:])
		os.Exit(0)
	}
	dir := t.TempDir()
	root := filepath.Join(dir, "leases")
	resource := filepath.Join(dir, "resource")
	child := exec.Command(os.Args[0], "-test.run=^TestOrdinaryLeaseAcrossServerRestart$")
	child.Env = append(os.Environ(), "UID_LEASE_HELPER=1", "UID_LEASE_ROOT="+root, "UID_LEASE_RESOURCE="+resource)
	input, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { child.Process.Kill(); child.Wait() }()
	var ready [6]byte
	if _, err := output.Read(ready[:]); err != nil || string(ready[:]) != "ready\n" {
		t.Fatalf("helper readiness %q %v", ready, err)
	}
	assertUnavailable := func() {
		t.Helper()
		p, err := NewPersistentUIDPool(10000, 10001, root)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		if uid, err := p.Acquire(ctx); err == nil {
			t.Fatalf("UID %d readmitted before cleanup proof", uid)
		}
	}
	// Remove the resource temporarily: only the owner's lifetime lock protects
	// the birth window before any UID-owned process or resource exists.
	if err := os.Remove(resource); err != nil {
		t.Fatal(err)
	}
	assertUnavailable()
	if err := os.WriteFile(resource, []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	child.Wait()
	assertUnavailable()
	if err := os.Remove(resource); err != nil {
		t.Fatal(err)
	}
	p, err := NewPersistentUIDPool(10000, 10001, root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	uid, err := p.Acquire(ctx)
	if err != nil || uid != 10000 {
		t.Fatalf("clean UID not recovered: %d %v", uid, err)
	}
	p.Release(uid)
}

func TestOrdinaryPersistentPoolRejectsLiveUID(t *testing.T) {
	p, err := NewPersistentUIDPool(os.Getuid(), os.Getuid()+1, filepath.Join(t.TempDir(), "leases"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if uid, err := p.Acquire(ctx); err == nil {
		t.Fatalf("live UID %d admitted after restart", uid)
	}
}

func TestOrdinaryLeaseRetainsResourcesOnRelease(t *testing.T) {
	dir := t.TempDir()
	p, err := NewPersistentUIDPool(10000, 10001, filepath.Join(dir, "leases"))
	if err != nil {
		t.Fatal(err)
	}
	uid, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	resource := filepath.Join(dir, "retained")
	if err := p.TrackResource(uid, resource); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resource, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	p.Release(uid)
	if p.Len() != 0 {
		t.Fatal("retained resources released UID")
	}
	if err := os.Remove(resource); err != nil {
		t.Fatal(err)
	}
	p.Release(uid)
	if p.Len() != 1 {
		t.Fatal("clean lease not released")
	}
}

func TestOrdinaryLeaseRejectsUnsafeMetadata(t *testing.T) {
	for _, kind := range []string{"root permissions", "root symlink", "record symlink", "record permissions", "corrupt record", "missing identity", "resource symlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			root := filepath.Join(dir, "leases")
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "root permissions":
				os.Chmod(root, 0755)
			case "root symlink":
				os.Remove(root)
				os.Symlink(dir, root)
			case "record symlink":
				os.Symlink(filepath.Join(dir, "absent"), filepath.Join(root, "10000.json"))
			case "record permissions":
				os.WriteFile(filepath.Join(root, "10000.json"), []byte(`{"uid":10000,"resources":[]}`), 0644)
			case "missing identity":
				os.WriteFile(filepath.Join(root, "10000.json"), []byte(`{}`), 0600)
			case "corrupt record":
				os.WriteFile(filepath.Join(root, "10000.json"), []byte(`{"uid":10000,"resources":[]`), 0600)
			case "resource symlink":
				resource := filepath.Join(dir, "resource")
				if err := os.Symlink(filepath.Join(dir, "missing"), resource); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "10000.json"), []byte(fmt.Sprintf(`{"uid":10000,"resources":[%q]}`, resource)), 0600); err != nil {
					t.Fatal(err)
				}
			}
			p, err := NewPersistentUIDPool(10000, 10001, root)
			if err != nil {
				if kind == "root permissions" || kind == "root symlink" {
					return
				}
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			if uid, err := p.Acquire(ctx); err == nil {
				t.Fatalf("unsafe metadata admitted UID %d", uid)
			}
		})
	}
}

func TestOrdinaryQuarantineKeepsHealthyUIDAvailable(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "leases")
	resource := filepath.Join(dir, "retained")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resource, []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "10000.json"), []byte(fmt.Sprintf(`{"uid":10000,"resources":[%q]}`, resource)), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := NewPersistentUIDPool(10000, 10002, root)
	if err != nil {
		t.Fatal(err)
	}
	uid, err := p.Acquire(context.Background())
	if err != nil || uid != 10001 {
		t.Fatalf("healthy UID unavailable: %d %v", uid, err)
	}
	p.Release(uid)
	if _, err := os.Stat(resource); err != nil {
		t.Fatal("recovery deleted retained resource", err)
	}
}
