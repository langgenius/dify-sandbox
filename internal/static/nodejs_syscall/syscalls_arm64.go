//go:build linux && arm64

package nodejs_syscall

import "syscall"

const (
	SYS_RSEQ = 293
	SYS_STATX = 291
	// 435
	SYS_CLONE3 = 435
	// 425, 426, 427 - io_uring syscalls for async I/O
	SYS_IO_URING_SETUP = 425
	SYS_IO_URING_ENTER = 426
	SYS_IO_URING_REGISTER = 427
)

var ALLOW_SYSCALLS = []int{
	// file
	syscall.SYS_CLOSE, syscall.SYS_WRITE, syscall.SYS_READ,
	syscall.SYS_FSTAT, syscall.SYS_FCNTL,
	syscall.SYS_READLINKAT, syscall.SYS_OPENAT,
	syscall.SYS_FSTATAT, syscall.SYS_LSEEK,
	syscall.SYS_DUP3,
	48, // SYS_FACCESSAT - check file accessibility
	67, // SYS_PREAD64
	68, // SYS_PWRITE64
	69, // SYS_PREADV
	70, // SYS_PWRITEV
	19, // SYS_EVENTFD2 - event notification for event loop

	// process
	syscall.SYS_GETPID, syscall.SYS_TGKILL, syscall.SYS_FUTEX, syscall.SYS_IOCTL,
	syscall.SYS_EXIT, syscall.SYS_EXIT_GROUP,
	syscall.SYS_SET_ROBUST_LIST, syscall.SYS_NANOSLEEP, syscall.SYS_SCHED_GETAFFINITY,
	syscall.SYS_SCHED_YIELD,

	// memory
	syscall.SYS_RT_SIGPROCMASK, syscall.SYS_SIGALTSTACK, syscall.SYS_RT_SIGACTION,
	syscall.SYS_MMAP, syscall.SYS_MUNMAP, syscall.SYS_MADVISE, syscall.SYS_MPROTECT,
	syscall.SYS_RT_SIGRETURN, syscall.SYS_BRK,
	syscall.SYS_MREMAP,

	//user/group
	syscall.SYS_SETUID, syscall.SYS_SETGID, syscall.SYS_GETTID,
	syscall.SYS_GETUID, syscall.SYS_GETGID,
	17, // SYS_GETCWD
	175, // SYS_GETEUID
	177, // SYS_GETEGID
	90, // SYS_CAPGET
	151, // SYS_SETFSUID
	152, // SYS_SETFSGID
	154, // SYS_SETPGID

	// epoll
	syscall.SYS_EPOLL_CREATE1,
	syscall.SYS_EPOLL_CTL, syscall.SYS_EPOLL_PWAIT,

	// time
	syscall.SYS_CLOCK_GETTIME, syscall.SYS_GETTIMEOFDAY, syscall.SYS_NANOSLEEP,
	syscall.SYS_PSELECT6,
	syscall.SYS_CLOCK_NANOSLEEP,
	153, // SYS_TIMES - process times

	// timer
	syscall.SYS_TIMERFD_CREATE, syscall.SYS_TIMERFD_SETTIME, syscall.SYS_TIMERFD_GETTIME,

	// process
	syscall.SYS_GETPPID,
	96, // SYS_SET_TID_ADDRESS - needed for thread operations on ARM64
	100, // SYS_SET_ROBUST_LIST - might be needed
	261, // SYS_PRLIMIT64 - needed for resource limits
	230, // SYS_MLOCKALL - memory locking
	231, // SYS_MUNLOCKALL
	232, // SYS_MINCORE

	// random
	syscall.SYS_GETRANDOM,

	// misc
	SYS_RSEQ,
	SYS_STATX,
	syscall.SYS_GETDENTS64,
	syscall.SYS_PIPE2,

	// threading
	syscall.SYS_CLONE,
	SYS_CLONE3,

	// additional syscalls that might be needed (using correct numbers)
	217,  // SYS_ADD_KEY
	218,  // SYS_REQUEST_KEY
	219,  // SYS_KEYCTL
	130,  // SYS_TKILL (not TGKILL which is 131)

	// io_uring - async I/O interface used by Node.js
	SYS_IO_URING_SETUP,
	SYS_IO_URING_ENTER,
	SYS_IO_URING_REGISTER,

	// execve - might be called during process operations
	221, // SYS_EXECVE

	// basic socket operations needed for Node.js initialization (even without network enabled)
	204, // SYS_GETSOCKNAME - needed by Node.js/V8 during startup
	209, // SYS_GETSOCKOPT - needed by Node.js/V8 during startup
}

var ALLOW_ERROR_SYSCALLS = []int{
	// SYS_CLONE moved to ALLOW_SYSCALLS as Node.js needs it for threading
}

var ALLOW_NETWORK_SYSCALLS = []int{
	syscall.SYS_SOCKET, syscall.SYS_CONNECT, syscall.SYS_BIND, syscall.SYS_LISTEN, syscall.SYS_ACCEPT,
	syscall.SYS_SENDTO, syscall.SYS_RECVFROM,
	syscall.SYS_GETSOCKNAME, syscall.SYS_SETSOCKOPT, syscall.SYS_GETSOCKOPT,
	syscall.SYS_SENDMMSG, syscall.SYS_RECVMSG, syscall.SYS_SENDMSG,
	syscall.SYS_GETPEERNAME, syscall.SYS_PPOLL, syscall.SYS_UNAME,
	syscall.SYS_RECVMMSG, syscall.SYS_SOCKETPAIR, syscall.SYS_SHUTDOWN,
	syscall.SYS_FSTATAT, syscall.SYS_FSTAT, syscall.SYS_LSEEK,
	syscall.SYS_FSTATFS,
}
