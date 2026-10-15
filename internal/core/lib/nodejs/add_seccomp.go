//go:build linux

package nodejs

import (
	"fmt"
	sg "github.com/seccomp/libseccomp-golang"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/langgenius/dify-sandbox/internal/core/lib"
	"github.com/langgenius/dify-sandbox/internal/static/nodejs_syscall"
)

//var allow_syscalls = []int{}

func InitSeccomp(uid int, gid int, enable_network bool) error {
	return initSeccomp(uid, gid, enable_network, false)
}

// InitSeccompIsolated enters the already isolated environment and private cwd
// before installing the same native filter. Network remains disabled.
func InitSeccompIsolated(uid, gid int) error       { return initSeccomp(uid, gid, false, true) }
func InitSeccompIsolatedScoped(uid, gid int) error { return initSeccomp(uid, gid, true, true) }

func initSeccomp(uid int, gid int, enable_network bool, isolated bool) error {
	root, cwd := ".", "/"
	if isolated {
		root, cwd = "/", "/work"
	}
	err := syscall.Chroot(root)
	if err != nil {
		return err
	}
	err = syscall.Chdir(cwd)
	if err != nil {
		return err
	}

	lib.SetNoNewPrivs()

	allowed_syscalls := []int{}
	allowed_not_kill_syscalls := []int{}

	allowed_syscall := ""
	if !isolated {
		allowed_syscall = os.Getenv("ALLOWED_SYSCALLS")
	}
	if allowed_syscall != "" {
		nums := strings.Split(allowed_syscall, ",")
		for num := range nums {
			syscall, err := strconv.Atoi(nums[num])
			if err != nil {
				continue
			}
			allowed_syscalls = append(allowed_syscalls, syscall)
		}
		allowed_syscalls = append(allowed_syscalls, syscall.SYS_SETGROUPS)
	} else {
		allowed_syscalls = append(allowed_syscalls, nodejs_syscall.ALLOW_SYSCALLS...)
		allowed_not_kill_syscalls = append(allowed_not_kill_syscalls, nodejs_syscall.ALLOW_ERROR_SYSCALLS...)

		if enable_network && !isolated {
			allowed_syscalls = append(allowed_syscalls, nodejs_syscall.ALLOW_NETWORK_SYSCALLS...)
		}
	}

	if isolated {
		extra, err := isolatedFilesystemSyscalls()
		if err != nil {
			return err
		}
		for _, number := range extra {
			found := false
			for _, existing := range allowed_syscalls {
				found = found || existing == number
			}
			if !found {
				allowed_syscalls = append(allowed_syscalls, number)
			}
		}
	}
	if isolated && enable_network {
		err = lib.SeccompIsolatedScoped(allowed_syscalls, allowed_not_kill_syscalls)
	} else if isolated {
		err = lib.SeccompIsolated(allowed_syscalls, allowed_not_kill_syscalls)
	} else {
		err = lib.Seccomp(allowed_syscalls, allowed_not_kill_syscalls)
	}
	if err != nil {
		return err
	}

	err = syscall.Setgroups([]int{})
	if err != nil {
		return err
	}

	// setgid
	err = syscall.Setgid(gid)
	if err != nil {
		return err
	}

	// setuid
	err = syscall.Setuid(uid)
	if err != nil {
		return err
	}

	return nil
}

// These additions permit cwd/metadata inspection and module discovery inside
// the read-only environment. They do not permit network, mount or cwd changes.
// Names are resolved by libseccomp for the actual target architecture.
func isolatedFilesystemSyscalls() ([]int, error) {
	result := make([]int, 0, 8)
	for _, name := range []string{"getcwd", "newfstatat", "readlinkat", "getdents64", "fstat", "lseek", "statfs", "fstatfs", "statx", "faccessat", "faccessat2"} {
		number, err := sg.GetSyscallFromName(name)
		if err != nil || number < 0 {
			return nil, fmt.Errorf("unsupported isolated filesystem syscall %s: %v", name, err)
		}
		result = append(result, int(number))
	}
	return result, nil
}
