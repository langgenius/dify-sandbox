//go:build linux && amd64

package nodejs_syscall

import "syscall"

var ALLOW_SYSCALLS = []int{
	syscall.SYS_OPEN, syscall.SYS_WRITE, syscall.SYS_CLOSE, syscall.SYS_READ,
	syscall.SYS_OPENAT, syscall.SYS_NEWFSTATAT, syscall.SYS_IOCTL, syscall.SYS_LSEEK,
	syscall.SYS_FSTAT,
	syscall.SYS_MPROTECT, syscall.SYS_MMAP, syscall.SYS_MUNMAP,
	syscall.SYS_BRK,
	syscall.SYS_RT_SIGACTION, syscall.SYS_RT_SIGPROCMASK,
	syscall.SYS_MADVISE, syscall.SYS_GETPID, syscall.SYS_GETUID,
	syscall.SYS_FCNTL, syscall.SYS_SIGALTSTACK,
	syscall.SYS_FUTEX,
	syscall.SYS_EXIT_GROUP,
	syscall.SYS_EPOLL_CTL,
	syscall.SYS_EPOLL_PWAIT,

	syscall.SYS_SETUID, syscall.SYS_SETGID,

	syscall.SYS_CLOCK_GETTIME, syscall.SYS_GETTIMEOFDAY, syscall.SYS_NANOSLEEP,
	syscall.SYS_TIME,

	syscall.SYS_TGKILL,

	syscall.SYS_READLINK,
	syscall.SYS_DUP3,
}

var ALLOW_NETWORK_SYSCALLS = []int{
	syscall.SYS_SOCKET, syscall.SYS_CONNECT, syscall.SYS_BIND, syscall.SYS_LISTEN, syscall.SYS_ACCEPT, syscall.SYS_SENDTO, syscall.SYS_RECVFROM,
	syscall.SYS_GETSOCKNAME, syscall.SYS_GETPEERNAME, syscall.SYS_SETSOCKOPT, syscall.SYS_PPOLL, syscall.SYS_UNAME,
}
