//go:build linux && arm64

package python_syscall

import "syscall"

const (
	SYS_GETRANDOM = 318
	SYS_RSEQ      = 334
)

var ALLOW_SYSCALLS = []int{
	// file io
	syscall.SYS_WRITE, syscall.SYS_CLOSE,
	// thread
	syscall.SYS_FUTEX,
	// memory
	syscall.SYS_MMAP, syscall.SYS_BRK, syscall.SYS_MPROTECT, syscall.SYS_MUNMAP, syscall.SYS_RT_SIGRETURN, syscall.SYS_RT_SIGPROCMASK,
	syscall.SYS_SIGALTSTACK,
	// user/group
	syscall.SYS_SETUID, syscall.SYS_SETGID,
	// process
	syscall.SYS_GETPID, syscall.SYS_GETPPID, syscall.SYS_GETTID,
	syscall.SYS_EXIT, syscall.SYS_EXIT_GROUP,
	syscall.SYS_TGKILL, syscall.SYS_RT_SIGACTION,
	// time
	syscall.SYS_CLOCK_GETTIME, syscall.SYS_GETTIMEOFDAY, syscall.SYS_NANOSLEEP,
	syscall.SYS_EPOLL_CTL, syscall.SYS_CLOCK_NANOSLEEP, syscall.SYS_PSELECT6,
	syscall.SYS_TIMERFD_CREATE, syscall.SYS_TIMERFD_SETTIME, syscall.SYS_TIMERFD_GETTIME,
}
