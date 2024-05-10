package nodejs

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"github.com/langgenius/dify-sandbox/internal/core/lib"
	"github.com/langgenius/dify-sandbox/internal/static/nodejs_syscall"
	sg "github.com/seccomp/libseccomp-golang"
)

const (
	seccompSetModeFilter   = 0x1
	seccompFilterFlagTSYNC = 0x1
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

	lib.SetNoNewPrivs()

	ctx, err := sg.NewFilter(sg.ActKillProcess)
	if err != nil {
		return err
	}

	allowed_syscall := os.Getenv("ALLOWED_SYSCALLS")
	if allowed_syscall != "" {
		nums := strings.Split(allowed_syscall, ",")
		for num := range nums {
			syscall, err := strconv.Atoi(nums[num])
			if err != nil {
				return err
			}
			err = ctx.AddRule(sg.ScmpSyscall(syscall), sg.ActAllow)
			if err != nil {
				return err
			}
		}
	} else {
		for _, syscall := range nodejs_syscall.ALLOW_SYSCALLS {
			err = ctx.AddRule(sg.ScmpSyscall(syscall), sg.ActAllow)
			if err != nil {
				return err
			}
		}

		for _, syscall := range nodejs_syscall.ALLOW_ERROR_SYSCALLS {
			err = ctx.AddRule(sg.ScmpSyscall(syscall), sg.ActErrno)
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
	}

	reader, writer, err := os.Pipe()
	if err != nil {
		return err
	}
	defer reader.Close()
	defer writer.Close()

	file := os.NewFile(uintptr(writer.Fd()), "pipe")
	ctx.ExportBPF(file)

	// read from pipe
	data := make([]byte, 4096)
	n, err := reader.Read(data)
	if err != nil {
		return err
	}
	// load bpf
	sock_filters := make([]syscall.SockFilter, n/8)
	bytesBuffer := bytes.NewBuffer(data)
	err = binary.Read(bytesBuffer, binary.LittleEndian, &sock_filters)
	if err != nil {
		return err
	}

	bpf := syscall.SockFprog{
		Len:    uint16(len(sock_filters)),
		Filter: &sock_filters[0],
	}

	_, _, err2 := syscall.Syscall(
		syscall.SYS_SECCOMP,
		uintptr(seccompSetModeFilter),
		uintptr(seccompFilterFlagTSYNC),
		uintptr(unsafe.Pointer(&bpf)),
	)

	if err2 != 0 {
		return errors.New("seccomp error")
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
