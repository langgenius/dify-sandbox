//go:build linux && (amd64 || arm64)

// Package iouring disables native io_uring before sandbox interpreters start.
package iouring

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

var disableOnce = sync.OnceValue(installFilter)

// Disable makes native io_uring syscalls return ENOSYS on all process threads
// and in future children, so runtimes fall back before loading sandbox filters.
// It sets no_new_privs, preventing exec from granting new privileges. Concurrent
// calls share one installation attempt and the same result, including errors.
func Disable() error {
	return disableOnce()
}

func installFilter() error {
	auditArch := uint32(0xc000003e) // AUDIT_ARCH_X86_64
	sysSeccomp := uintptr(317)
	if runtime.GOARCH == "arm64" {
		auditArch = 0xc00000b7 // AUDIT_ARCH_AARCH64
		sysSeccomp = 277
	}

	// no_new_privs is per-thread until TSYNC propagates it with the filter.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	const (
		prSetNoNewPrivs        = 38
		seccompSetModeFilter   = 1
		seccompFilterFlagTSYNC = 1
		seccompRetErrno        = 0x00050000
		seccompRetAllow        = 0x7fff0000
	)
	if _, _, errno := syscall.Syscall6(syscall.SYS_PRCTL, prSetNoNewPrivs, 1, 0, 0, 0, 0); errno != 0 {
		return fmt.Errorf("set no_new_privs before disabling io_uring: %w", errno)
	}

	// Other ABIs remain subject to the child's sandbox filter. Native syscall
	// numbers 425 through 427 are io_uring_setup, enter and register on both CPUs.
	filters := []syscall.SockFilter{
		{Code: syscall.BPF_LD | syscall.BPF_W | syscall.BPF_ABS, K: 4}, // seccomp_data.arch
		{Code: syscall.BPF_JMP | syscall.BPF_JEQ | syscall.BPF_K, K: auditArch, Jf: 4},
		{Code: syscall.BPF_LD | syscall.BPF_W | syscall.BPF_ABS, K: 0}, // seccomp_data.nr
		{Code: syscall.BPF_JMP | syscall.BPF_JGE | syscall.BPF_K, K: 425, Jf: 2},
		{Code: syscall.BPF_JMP | syscall.BPF_JGT | syscall.BPF_K, K: 427, Jt: 1},
		{Code: syscall.BPF_RET | syscall.BPF_K, K: seccompRetErrno | uint32(syscall.ENOSYS)},
		{Code: syscall.BPF_RET | syscall.BPF_K, K: seccompRetAllow},
	}
	prog := syscall.SockFprog{
		Len:    uint16(len(filters)),
		Filter: &filters[0],
	}
	tid, _, errno := syscall.Syscall(sysSeccomp, seccompSetModeFilter, seccompFilterFlagTSYNC, uintptr(unsafe.Pointer(&prog)))
	if errno != 0 {
		return fmt.Errorf("install io_uring seccomp filter: %w", errno)
	}
	// TSYNC can return the unsynchronized thread ID without setting errno.
	if tid != 0 {
		return fmt.Errorf("synchronize io_uring seccomp filter: thread %d could not synchronize", tid)
	}
	return nil
}
