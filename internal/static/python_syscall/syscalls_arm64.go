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
	syscall.SYS_WRITE, syscall.SYS_CLOSE, syscall.SYS_OPENAT, syscall.SYS_READ, syscall.SYS_LSEEK, syscall.SYS_GETDENTS64, syscall.SYS_WRITEV,
	syscall.SYS_CHDIR, syscall.SYS_FSTAT, syscall.SYS_FCNTL, syscall.SYS_PIPE2,
	syscall.SYS_DUP, syscall.SYS_DUP2, syscall.SYS_DUP3,

	// thread
	syscall.SYS_FUTEX,

	// memory
	syscall.SYS_MMAP, syscall.SYS_BRK, syscall.SYS_MPROTECT, syscall.SYS_MUNMAP, syscall.SYS_RT_SIGRETURN, syscall.SYS_RT_SIGPROCMASK,
	syscall.SYS_SIGALTSTACK, syscall.SYS_MREMAP, syscall.SYS_MADVISE,

	// user/group
	syscall.SYS_SETUID, syscall.SYS_SETGID, syscall.SYS_GETUID,

	// process
	syscall.SYS_GETPID, syscall.SYS_GETPPID, syscall.SYS_GETTID,
	syscall.SYS_EXIT, syscall.SYS_EXIT_GROUP,
	syscall.SYS_TGKILL, syscall.SYS_RT_SIGACTION,
	syscall.SYS_IOCTL, syscall.SYS_SCHED_YIELD,
	syscall.SYS_GET_ROBUST_LIST, syscall.SYS_SET_ROBUST_LIST,
	SYS_RSEQ, syscall.SYS_PRLIMIT64, syscall.SYS_SYSINFO,
	syscall.SYS_SET_TID_ADDRESS,
	syscall.SYS_GETEUID, syscall.SYS_GETEGID, syscall.SYS_GETRESUID, syscall.SYS_GETRESGID,
	syscall.SYS_PRCTL, syscall.SYS_SCHED_GETAFFINITY, syscall.SYS_FADVISE64, syscall.SYS_READLINK,
	syscall.SYS_UNAME, syscall.SYS_FSTATFS, syscall.SYS_FACCESSAT,

	// time
	syscall.SYS_EPOLL_CREATE1,
	syscall.SYS_CLOCK_GETTIME, syscall.SYS_GETTIMEOFDAY, syscall.SYS_NANOSLEEP,
	syscall.SYS_EPOLL_CTL, syscall.SYS_CLOCK_NANOSLEEP, syscall.SYS_PSELECT6,
	syscall.SYS_TIMERFD_CREATE, syscall.SYS_TIMERFD_SETTIME, syscall.SYS_TIMERFD_GETTIME,

	// get random
	syscall.SYS_GETRANDOM,
	syscall.SYS_PPOLL, syscall.SYS_EPOLL_PWAIT,
}

var ALLOW_ERROR_SYSCALLS = []int{
	syscall.SYS_CLONE,
	syscall.SYS_MKDIRAT,
	syscall.SYS_SOCKET,
}

var ALLOW_NETWORK_SYSCALLS = []int{
	syscall.SYS_SOCKET, syscall.SYS_CONNECT, syscall.SYS_BIND, syscall.SYS_LISTEN, syscall.SYS_ACCEPT, syscall.SYS_SENDTO,
	syscall.SYS_RECVFROM, syscall.SYS_RECVMSG, syscall.SYS_GETSOCKOPT,
	syscall.SYS_GETSOCKNAME, 	syscall.SYS_SENDMSG, SYS_SENDMMSG, syscall.SYS_GETSOCKOPT,
	syscall.SYS_FSTATAT,
}
```
