//go:build linux && arm64

package lib

import "syscall"

const (
	SYS_SECCOMP = syscall.SYS_SECCOMP
	SYS_CLONE3  = 435
)
