package nodejs

import (
	"os"
	"strconv"
	"syscall"

	"github.com/langgenius/dify-sandbox/internal/static/nodejs_syscall"
	sg "github.com/seccomp/libseccomp-golang"
)

// var allow_syscalls = []int{}

func InitSeccomp(uid int, gid int, enable_network bool) error {
	err := syscall.Chroot(".")
	if err != nil {
		return err
	}
	err = syscall.Chdir("/")
	if err != nil {
		return err
	}

	disabled_syscall, err := strconv.Atoi(os.Getenv("DISABLE_SYSCALL"))
	if err != nil {
		disabled_syscall = -1
	}

	ctx, err := sg.NewFilter(sg.ActKillProcess)
	if err != nil {
		return err
	}
	defer ctx.Release()

	for _, syscall := range nodejs_syscall.ALLOW_SYSCALLS {
		if syscall == disabled_syscall {
			continue
		}
		err = ctx.AddRule(sg.ScmpSyscall(syscall), sg.ActAllow)
		if err != nil {
			return err
		}
	}

	if enable_network {
		for _, syscall := range nodejs_syscall.ALLOW_NETWORK_SYSCALLS {
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
