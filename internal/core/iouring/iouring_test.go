//go:build linux && (amd64 || arm64)

package iouring_test

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"github.com/langgenius/dify-sandbox/internal/core/iouring"
)

func TestDisable(t *testing.T) {
	const childEnv = "DIFY_SANDBOX_TEST_IOURING_CHILD"
	if os.Getenv(childEnv) == "1" {
		// Do not install a filter here: this must be inherited across execve.
		assertIOUringDisabled(t)
		return
	}

	for i := 0; i < 2; i++ {
		if err := iouring.Disable(); err != nil {
			t.Fatalf("Disable call %d: %v", i+1, err)
		}
	}
	assertIOUringDisabled(t)

	cmd := exec.Command(os.Args[0], "-test.run=^TestDisable$")
	cmd.Env = append(os.Environ(), childEnv+"=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child process did not inherit io_uring filter: %v\n%s", err, output)
	}
}

func TestDisableTSYNCFailure(t *testing.T) {
	const childEnv = "DIFY_SANDBOX_TEST_IOURING_TSYNC_FAILURE"
	if os.Getenv(childEnv) != "1" {
		// Keep the divergent filter and cached failure out of the test parent.
		cmd := exec.Command(os.Args[0], "-test.run=^TestDisableTSYNCFailure$")
		cmd.Env = append(os.Environ(), childEnv+"=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("TSYNC failure child process: %v\n%s", err, output)
		}
		return
	}

	ready := make(chan error)
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		// Exiting while locked retires the thread with its divergent filter.
		defer close(done)
		ready <- installThreadAllowFilter()
		<-release
	}()
	defer func() {
		close(release)
		<-done
	}()
	if err := <-ready; err != nil {
		t.Fatalf("install divergent thread filter: %v", err)
	}

	first := iouring.Disable()
	second := iouring.Disable()
	for i, err := range []error{first, second} {
		if err == nil || !strings.Contains(err.Error(), "could not synchronize") {
			t.Fatalf("Disable call %d returned %v, want a TSYNC synchronization failure", i+1, err)
		}
	}
	if first != second {
		t.Fatalf("Disable did not cache its failure: first=%v, second=%v", first, second)
	}
}

// installThreadAllowFilter creates a divergent filter tree on a locked thread.
// PR_SET_SECCOMP installs only on the caller, leaving the other threads unchanged.
func installThreadAllowFilter() error {
	const (
		prSetNoNewPrivs   = 38
		seccompModeFilter = 2
		seccompRetAllow   = 0x7fff0000
	)
	if _, _, errno := syscall.Syscall6(syscall.SYS_PRCTL, prSetNoNewPrivs, 1, 0, 0, 0, 0); errno != 0 {
		return errno
	}
	filter := syscall.SockFilter{Code: syscall.BPF_RET | syscall.BPF_K, K: seccompRetAllow}
	prog := syscall.SockFprog{Len: 1, Filter: &filter}
	if _, _, errno := syscall.Syscall6(syscall.SYS_PRCTL, syscall.PR_SET_SECCOMP, seccompModeFilter, uintptr(unsafe.Pointer(&prog)), 0, 0, 0); errno != 0 {
		return errno
	}
	return nil
}

func assertIOUringDisabled(t *testing.T) {
	t.Helper()
	for _, nr := range []uintptr{425, 426, 427} {
		if _, _, errno := syscall.Syscall6(nr, 0, 0, 0, 0, 0, 0); errno != syscall.ENOSYS {
			t.Errorf("syscall %d returned %v, want ENOSYS", nr, errno)
		}
	}
}
