package nodejs

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"syscall"
	"unsafe"

	"github.com/langgenius/dify-sandbox/internal/static/nodejs_syscall"
	sg "github.com/seccomp/libseccomp-golang"
)

// var allow_syscalls = []int{}

func InitSeccomp(uid int, gid int) error {
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

	// for i := 0; i < 400; i++ {
	// 	allow_syscalls = append(allow_syscalls, i)
	// }

	for _, syscall := range nodejs_syscall.ALLOW_SYSCALLS {
		if syscall == disabled_syscall {
			continue
		}
		err = ctx.AddRule(sg.ScmpSyscall(syscall), sg.ActAllow)
		if err != nil {
			return err
		}
	}

	for _, syscall := range nodejs_syscall.ERROR_CODE_SYSCALLS {
		if syscall == disabled_syscall {
			continue
		}
		err = ctx.AddRule(sg.ScmpSyscall(syscall), sg.ActErrno)
		if err != nil {
			return err
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

	_, _, err2 := syscall.RawSyscall6(syscall.SYS_PRCTL, syscall.PR_SET_SECCOMP, 2, uintptr(unsafe.Pointer(&bpf)), 0, 0, 0)
	if err2 != 0 {
		return fmt.Errorf("prctl failed: %v", err2)
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
