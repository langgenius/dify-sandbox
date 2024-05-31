//go:build linux && arm64

package nodejs_syscall

import "syscall"

var ALLOW_SYSCALLS = []int{
	// file
	syscall.SYS_CLOSE, syscall.SYS_WRITE, syscall.SYS_READ,
	syscall.SYS_FSTAT, syscall.SYS_FCNTL,
	syscall.SYS_READLINKAT, syscall.SYS_OPENAT,

	// process
	syscall.SYS_GETPID, syscall.SYS_TGKILL, syscall.SYS_FUTEX, syscall.SYS_IOCTL,
	syscall.SYS_EXIT, syscall.SYS_EXIT_GROUP,
	syscall.SYS_SET_ROBUST_LIST, syscall.SYS_NANOSLEEP, syscall.SYS_SCHED_GETAFFINITY,
	syscall.SYS_SCHED_YIELD,

	// memory
	syscall.SYS_RT_SIGPROCMASK, syscall.SYS_SIGALTSTACK, syscall.SYS_RT_SIGACTION,
	syscall.SYS_MMAP, syscall.SYS_MUNMAP, syscall.SYS_MADVISE, syscall.SYS_MPROTECT,
	syscall.SYS_RT_SIGRETURN, syscall.SYS_BRK,

	//user/group
	syscall.SYS_SETUID, syscall.SYS_SETGID, syscall.SYS_GETTID,
	syscall.SYS_GETUID, syscall.SYS_GETGID,

	// epoll
	syscall.SYS_EPOLL_CTL, syscall.SYS_EPOLL_PWAIT,
}

var ALLOW_ERROR_SYSCALLS = []int{
	syscall.SYS_CLONE, 293,
}

var ALLOW_NETWORK_SYSCALLS = []int{
	syscall.SYS_SOCKET, syscall.SYS_CONNECT, syscall.SYS_BIND, syscall.SYS_LISTEN, syscall.SYS_ACCEPT,
	syscall.SYS_SENDTO, syscall.SYS_RECVFROM,
	syscall.SYS_GETSOCKNAME, syscall.SYS_SETSOCKOPT, syscall.SYS_GETSOCKOPT,
	syscall.SYS_SENDMMSG, syscall.SYS_RECVMSG,
	syscall.SYS_GETPEERNAME, syscall.SYS_PPOLL, syscall.SYS_UNAME,
	syscall.SYS_FSTATAT, syscall.SYS_LSEEK,
	syscall.SYS_FSTATFS,
}
