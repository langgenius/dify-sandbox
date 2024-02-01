package static

import "syscall"

const (
	SYS_GETRANDOM = 318
	SYS_RSEQ      = 334
)

var ALLOW_SYSCALLS = []int{
	// file io
	syscall.SYS_READ, syscall.SYS_WRITE, syscall.SYS_OPEN, syscall.SYS_OPENAT, syscall.SYS_CLOSE,
	syscall.SYS_PREAD64, syscall.SYS_PWRITE64, syscall.SYS_ACCESS, syscall.SYS_NEWFSTATAT, syscall.SYS_SET_TID_ADDRESS, syscall.SYS_SET_ROBUST_LIST, syscall.SYS_PRLIMIT64,
	SYS_RSEQ, SYS_GETRANDOM,
	syscall.SYS_LSEEK, syscall.SYS_IOCTL, syscall.SYS_GETDENTS, syscall.SYS_GETDENTS64, syscall.SYS_FUTEX, syscall.SYS_READLINK, syscall.SYS_SYSINFO, syscall.SYS_FCNTL,
	syscall.SYS_DUP,
	// memory
	syscall.SYS_MMAP, syscall.SYS_BRK, syscall.SYS_MPROTECT, syscall.SYS_MUNMAP,
	// user/group
	syscall.SYS_GETUID, syscall.SYS_GETEUID, syscall.SYS_GETGID, syscall.SYS_SETUID, syscall.SYS_SETGID, syscall.SYS_GETEGID,
	// process
	syscall.SYS_GETPID, syscall.SYS_GETPPID, syscall.SYS_GETTID,
	syscall.SYS_CLONE, syscall.SYS_FORK, syscall.SYS_VFORK, syscall.SYS_EXECVE, syscall.SYS_EXIT, syscall.SYS_EXIT_GROUP,
	syscall.SYS_WAIT4, syscall.SYS_WAITID,
	syscall.SYS_KILL, syscall.SYS_TKILL, syscall.SYS_TGKILL, syscall.SYS_RT_SIGQUEUEINFO, syscall.SYS_RT_SIGPROCMASK, syscall.SYS_RT_SIGRETURN, syscall.SYS_RT_SIGACTION,
	// time
	syscall.SYS_CLOCK_GETTIME, syscall.SYS_GETTIMEOFDAY, syscall.SYS_TIME, syscall.SYS_NANOSLEEP,

	syscall.SYS_ARCH_PRCTL,
}
