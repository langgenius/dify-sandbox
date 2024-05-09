package python

import (
	"syscall"

	"github.com/langgenius/dify-sandbox/internal/static/python_syscall"
	sg "github.com/seccomp/libseccomp-golang"
)

//var allow_syscalls = []int{}

func InitSeccomp(uid int, gid int, enable_network bool) error {
	err := syscall.Chroot(".")
	if err != nil {
		return err
	}
	err = syscall.Chdir("/")
	if err != nil {
		return err
	}

	ctx, err := sg.NewFilter(sg.ActKillProcess)
	if err != nil {
		return err
	}

	for _, syscall := range python_syscall.ALLOW_SYSCALLS {
		err = ctx.AddRule(sg.ScmpSyscall(syscall), sg.ActAllow)
		if err != nil {
			return err
		}
	}

	if enable_network {
		for _, syscall := range python_syscall.ALLOW_NETWORK_SYSCALLS {
			err = ctx.AddRule(sg.ScmpSyscall(syscall), sg.ActAllow)
			if err != nil {
				return err
			}
		}
	}

	err = ctx.Load()
	if err != nil {
		return err
	}

	// setuid
	err = syscall.Setuid(uid)
	if err != nil {
		return err
	}

	// setgid
	err = syscall.Setgid(gid)
	if err != nil {
		return err
	}

	return nil
}
