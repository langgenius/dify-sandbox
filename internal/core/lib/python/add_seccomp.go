//go:build linux

package python

import (
	"syscall"

	"github.com/langgenius/dify-sandbox/internal/core/lib"
	"github.com/langgenius/dify-sandbox/internal/static/python_syscall"
)

func InitSeccomp(uid int, gid int, enable_network bool) error {
	err := syscall.Chroot(".")
	if err != nil {
		return err
	}
	err = syscall.Chdir("/")
	if err != nil {
		return err
	}

	lib.SetNoNewPrivs()

	allowed_syscalls := append([]int{}, python_syscall.ALLOW_SYSCALLS...)
	allowed_not_kill_syscalls := append([]int{}, python_syscall.ALLOW_ERROR_SYSCALLS...)
	if enable_network {
		allowed_syscalls = append(allowed_syscalls, python_syscall.ALLOW_NETWORK_SYSCALLS...)
	}

	allowed_syscalls = lib.MergeSyscalls(allowed_syscalls, lib.SyscallsFromEnv("ALLOWED_SYSCALLS"))
	allowed_syscalls = lib.MergeSyscalls(allowed_syscalls, []int{syscall.SYS_SETGROUPS})

	err = lib.Seccomp(allowed_syscalls, allowed_not_kill_syscalls)
	if err != nil {
		return err
	}

	err = syscall.Setgroups([]int{})
	if err != nil {
		return err
	}

	err = syscall.Setgid(gid)
	if err != nil {
		return err
	}

	err = syscall.Setuid(uid)
	if err != nil {
		return err
	}

	return nil
}
