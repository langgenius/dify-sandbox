//go:build linux && amd64

package nodejs_syscall

import (
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	//334
	SYS_RSEQ = 334
	// 435
	SYS_CLONE3 = 435

	SYS_SENDMMSG      = 307
	SYS_GETRANDOM     = 318
	SYS_PKEY_ALLOC    = 329
	SYS_PKEY_MPROTECT = 330
	SYS_PKEY_FREE     = 331
	SYS_STATX         = 332
)

var ALLOW_SYSCALLS = []int{
	syscall.SYS_OPEN, syscall.SYS_WRITE, syscall.SYS_CLOSE, syscall.SYS_READ,
	syscall.SYS_OPENAT, syscall.SYS_NEWFSTATAT, syscall.SYS_IOCTL, syscall.SYS_LSEEK,
	syscall.SYS_FSTAT,
	syscall.SYS_MPROTECT, syscall.SYS_MMAP, syscall.SYS_MUNMAP,
	syscall.SYS_MREMAP,
	syscall.SYS_BRK,
	syscall.SYS_RT_SIGACTION, syscall.SYS_RT_SIGPROCMASK,
	syscall.SYS_MADVISE, syscall.SYS_GETPID, syscall.SYS_GETUID,
	syscall.SYS_FCNTL, syscall.SYS_SIGALTSTACK, syscall.SYS_RT_SIGRETURN,
	syscall.SYS_FUTEX,
	syscall.SYS_EXIT_GROUP,
	syscall.SYS_EPOLL_CTL,
	syscall.SYS_EPOLL_PWAIT,
	syscall.SYS_SCHED_YIELD, syscall.SYS_EXIT,
	syscall.SYS_SCHED_GETAFFINITY, syscall.SYS_SET_ROBUST_LIST,
	SYS_RSEQ,

	syscall.SYS_SETUID, syscall.SYS_SETGID, syscall.SYS_GETTID,
	syscall.SYS_CLOCK_GETTIME, syscall.SYS_GETTIMEOFDAY, syscall.SYS_NANOSLEEP,
	syscall.SYS_TIME,

	syscall.SYS_TGKILL,

	syscall.SYS_READLINK,
	syscall.SYS_DUP3,

	syscall.SYS_PIPE2,
	syscall.SYS_SET_TID_ADDRESS,
	syscall.SYS_PREAD64, syscall.SYS_PWRITE64,

	SYS_GETRANDOM,
	syscall.SYS_EPOLL_CREATE1, syscall.SYS_EVENTFD2,
}

var ALLOW_ERROR_SYSCALLS = []int{
	SYS_CLONE3, /* return ENOSYS for glibc */
}

var ALLOW_NETWORK_SYSCALLS = []int{
	syscall.SYS_SOCKET, syscall.SYS_CONNECT, syscall.SYS_BIND, syscall.SYS_LISTEN, syscall.SYS_ACCEPT, syscall.SYS_SENDTO, syscall.SYS_RECVFROM,
	syscall.SYS_GETSOCKNAME, syscall.SYS_RECVMSG, syscall.SYS_GETPEERNAME, syscall.SYS_SETSOCKOPT, syscall.SYS_PPOLL, syscall.SYS_UNAME,
	syscall.SYS_SENDMSG, syscall.SYS_GETSOCKOPT,
	syscall.SYS_FCNTL, syscall.SYS_FSTATFS,
	SYS_SENDMMSG, syscall.SYS_POLL,
	SYS_PKEY_ALLOC, SYS_PKEY_MPROTECT, SYS_PKEY_FREE, SYS_STATX,
	syscall.SYS_CLONE,
}

var ALLOW_NETWORK_SYSCALL_VALUES = map[int]uint64{
	// allow clone for nodejs networking
	syscall.SYS_CLONE: unix.CLONE_VM | unix.CLONE_FS | unix.CLONE_FILES | unix.CLONE_SIGHAND | unix.CLONE_THREAD | unix.CLONE_SYSVSEM | unix.CLONE_SETTLS | unix.CLONE_PARENT_SETTID | unix.CLONE_CHILD_CLEARTID,
}
