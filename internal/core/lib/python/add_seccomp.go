package python

import (
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com.langgenius/dify-sandbox/internal/core/lib"
	"github.com.langgenius/dify-sandbox/internal.static.python_syscall"
)

//var allow_syscalls = []int{}

func InitSeccomp(uid int, gid int, enable_network bool) error {
	// Change to a new root directory to prevent access to unsafe file systems
	err := syscall.Chroot(".")
	if err != nil {
		return err
	}
	// Change to the root directory to ensure operations are performed in the new root directory
	err = syscall.Chdir("/")
	if err != nil {
		return err
	}

	// Set no new privileges
	lib.SetNoNewPrivs()

	allowed_syscalls := []int{}
	allowed_not_kill_syscalls := []int{}
	// Allow some system calls that may return errors
	allowed_not_kill_syscalls = append(allowed_not_kill_syscalls, python_syscall.ALLOW_ERROR_SYSCALLS...)

	// Get allowed system calls from environment variables
	allowed_syscall := os.Getenv("ALLOWED_SYSCALLS")
	if allowed_syscall != "" {
		nums := strings.Split(allowed_syscall, ",")
		for num := range nums {
			syscall, err := strconv.Atoi(nums[num])
			if err != nil {
				continue
			}
			allowed_syscalls = append(allowed_syscalls, syscall)
		}
	} else {
		// Default allowed system calls
		allowed_syscalls = append(allowed_syscalls, python_syscall.ALLOW_SYSCALLS...)
		if enable_network {
			// If network is enabled, allow network-related system calls
			allowed_syscalls = append(allowed_syscalls, python_syscall.ALLOW_NETWORK_SYSCALLS...)
		}
	}
	// Set seccomp filter to restrict system calls
	// Requires root privileges
	err = lib.Seccomp(allowed_syscalls, allowed_not_kill_syscalls)
	if err != nil {
		return err
	}

	// Set user ID to prevent privilege escalation
	err = syscall.Setuid(uid)
	if err != nil {
		return err
	}

	// Set group ID to prevent privilege escalation
	err = syscall.Setgid(gid)
	if err != nil {
		return err
	}

	return nil

// Note: The above operations will not be successfully executed without root privileges. Specifically, system calls such as syscall.Chroot and syscall.Setuid require root privileges to execute. Without root privileges, the program will not be able to change the root directory or set user and group IDs, thus failing to effectively restrict privileges and system calls. This poses a security risk as the program may access unsafe file systems or escalate privileges.
