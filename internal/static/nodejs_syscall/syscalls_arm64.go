//go:build linux && arm64

package nodejs_syscall

import "syscall"

var ALLOW_SYSCALLS = []int{
	// file
	syscall.SYS_CLOSE, syscall.SYS_READ, syscall.SYS_WRITE, syscall.SYS_OPENAT,
	syscall.SYS_FSTAT, syscall.SYS_FCNTL,
	syscall.SYS_READLINKAT, syscall.SYS_FSTATAT,

	// io
	syscall.SYS_IOCTL,

	// process
	syscall.SYS_GETPID, syscall.SYS_TGKILL, syscall.SYS_FUTEX, syscall.SYS_EXIT_GROUP,

	// memory
	syscall.SYS_RT_SIGPROCMASK, syscall.SYS_SIGALTSTACK, syscall.SYS_RT_SIGACTION,
	syscall.SYS_MMAP, syscall.SYS_MUNMAP, syscall.SYS_MADVISE, syscall.SYS_MPROTECT,

	//user/group
	syscall.SYS_SETUID, syscall.SYS_SETGID,
	syscall.SYS_GETUID, syscall.SYS_GETGID,

	// epoll
	syscall.SYS_EPOLL_CTL, syscall.SYS_EPOLL_PWAIT,
}
