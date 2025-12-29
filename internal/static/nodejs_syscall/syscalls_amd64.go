//go:build linux && amd64

package nodejs_syscall

import "syscall"

const (
	//334
	SYS_RSEQ = 334
	// 435
	SYS_CLONE3 = 435
	// 318
	SYS_GETRANDOM = 318
	// 307
	SYS_SENDMMSG = 307
	// 243
	SYS_RECVMMSG = 243
)

var ALLOW_SYSCALLS = []int{
	// file
	syscall.SYS_OPEN, syscall.SYS_WRITE, syscall.SYS_CLOSE, syscall.SYS_READ,
	syscall.SYS_OPENAT, syscall.SYS_NEWFSTATAT, syscall.SYS_IOCTL, syscall.SYS_LSEEK,
	syscall.SYS_FSTAT, syscall.SYS_FCNTL,
	syscall.SYS_DUP3,
	syscall.SYS_GETDENTS64,
	syscall.SYS_PIPE2,
	89, // SYS_READLINK - using raw number for AMD64

	// process
	syscall.SYS_GETPID, syscall.SYS_TGKILL, syscall.SYS_FUTEX,
	syscall.SYS_EXIT, syscall.SYS_EXIT_GROUP,
	syscall.SYS_SET_ROBUST_LIST, syscall.SYS_NANOSLEEP, syscall.SYS_SCHED_GETAFFINITY,
	syscall.SYS_SCHED_YIELD,

	// memory
	syscall.SYS_MPROTECT, syscall.SYS_MMAP, syscall.SYS_MUNMAP,
	syscall.SYS_MREMAP, syscall.SYS_BRK,
	syscall.SYS_RT_SIGACTION, syscall.SYS_RT_SIGPROCMASK,
	syscall.SYS_MADVISE,
	syscall.SYS_SIGALTSTACK, syscall.SYS_RT_SIGRETURN,

	// user/group
	syscall.SYS_SETUID, syscall.SYS_SETGID, syscall.SYS_GETTID,
	syscall.SYS_GETUID,

	// epoll
	syscall.SYS_EPOLL_CREATE1,
	syscall.SYS_EPOLL_CTL, syscall.SYS_EPOLL_PWAIT,

	// time
	syscall.SYS_CLOCK_GETTIME, syscall.SYS_GETTIMEOFDAY, syscall.SYS_NANOSLEEP,
	syscall.SYS_PSELECT6,
	syscall.SYS_TIME,

	// random
	SYS_GETRANDOM,

	// misc
	SYS_RSEQ,

	// threading
	syscall.SYS_CLONE,
	SYS_CLONE3,
}

var ALLOW_ERROR_SYSCALLS = []int{
	// SYS_CLONE and SYS_CLONE3 moved to ALLOW_SYSCALLS as Node.js needs them for threading
}

var ALLOW_NETWORK_SYSCALLS = []int{
	syscall.SYS_SOCKET, syscall.SYS_CONNECT, syscall.SYS_BIND, syscall.SYS_LISTEN, syscall.SYS_ACCEPT,
	syscall.SYS_SENDTO, syscall.SYS_RECVFROM,
	syscall.SYS_GETSOCKNAME, syscall.SYS_SETSOCKOPT, syscall.SYS_GETSOCKOPT,
	SYS_SENDMMSG, syscall.SYS_RECVMSG,
	syscall.SYS_SENDMSG,
	syscall.SYS_GETPEERNAME, syscall.SYS_PPOLL, syscall.SYS_UNAME,
	SYS_RECVMMSG, syscall.SYS_SOCKETPAIR, syscall.SYS_SHUTDOWN,
	syscall.SYS_FCNTL, syscall.SYS_FSTAT, syscall.SYS_FSTATFS,
	syscall.SYS_POLL,
}
