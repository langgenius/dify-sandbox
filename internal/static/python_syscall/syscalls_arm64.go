//go:build linux && arm64

package python_syscall

import (
	"syscall"
)

const (
	SYS_RSEQ = 293
)

var ALLOW_SYSCALLS = []int{
	// file io
	syscall.SYS_WRITE, syscall.SYS_CLOSE, syscall.SYS_OPENAT, syscall.SYS_READ, syscall.SYS_LSEEK, syscall.SYS_GETDENTS64,

	// thread
	syscall.SYS_FUTEX,

	// memory
	syscall.SYS_MMAP, syscall.SYS_BRK, syscall.SYS_MPROTECT, syscall.SYS_MUNMAP, syscall.SYS_RT_SIGRETURN, syscall.SYS_RT_SIGPROCMASK,
	syscall.SYS_SIGALTSTACK, syscall.SYS_MREMAP,

	// user/group
	syscall.SYS_SETUID, syscall.SYS_SETGID, syscall.SYS_GETUID,

	// process
	syscall.SYS_GETPID, syscall.SYS_GETPPID, syscall.SYS_GETTID,
	syscall.SYS_EXIT, syscall.SYS_EXIT_GROUP,
	syscall.SYS_TGKILL, syscall.SYS_RT_SIGACTION,
	syscall.SYS_IOCTL, syscall.SYS_SCHED_YIELD,
	syscall.SYS_GET_ROBUST_LIST, syscall.SYS_SET_ROBUST_LIST,
	SYS_RSEQ,

	// time
	syscall.SYS_EPOLL_CREATE1,
	syscall.SYS_CLOCK_GETTIME, syscall.SYS_GETTIMEOFDAY, syscall.SYS_NANOSLEEP,
	syscall.SYS_EPOLL_CTL, syscall.SYS_CLOCK_NANOSLEEP, syscall.SYS_PSELECT6,
	syscall.SYS_TIMERFD_CREATE, syscall.SYS_TIMERFD_SETTIME, syscall.SYS_TIMERFD_GETTIME,

	// get random
	syscall.SYS_GETRANDOM,
}

var ALLOW_ERROR_SYSCALLS = []int{
	syscall.SYS_CLONE,
	syscall.SYS_MKDIRAT,
}

var ALLOW_NETWORK_SYSCALLS = []int{
	syscall.SYS_SOCKET, syscall.SYS_CONNECT, syscall.SYS_BIND, syscall.SYS_LISTEN, syscall.SYS_ACCEPT, syscall.SYS_SENDTO,
	syscall.SYS_RECVFROM, syscall.SYS_RECVMSG, syscall.SYS_GETSOCKOPT,
	syscall.SYS_GETSOCKNAME, syscall.SYS_GETPEERNAME, syscall.SYS_SETSOCKOPT,
	syscall.SYS_PPOLL, syscall.SYS_UNAME, syscall.SYS_SENDMMSG,
	syscall.SYS_FSTATAT, syscall.SYS_FSTAT, syscall.SYS_FSTATFS, syscall.SYS_EPOLL_PWAIT,
}
