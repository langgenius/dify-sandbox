//go:build linux

package lib

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"syscall"
	"unsafe"

	sg "github.com/seccomp/libseccomp-golang"
)

func Seccomp(allowed_syscalls []int, allowed_not_kill_syscalls []int) error {
	return seccomp(allowed_syscalls, allowed_not_kill_syscalls, false)
}

// SeccompIsolated lets optional dependency network probes observe a real denial.
// It never permits socket creation; ordinary errno behavior stays unchanged.
func SeccompIsolated(allowed, legacyErrno []int) error {
	for _, list := range [][]int{allowed, legacyErrno} {
		for _, number := range list {
			if number == syscall.SYS_SOCKET {
				return fmt.Errorf("isolated socket denial cannot be overridden")
			}
		}
	}
	return seccomp(allowed, legacyErrno, true)
}

func seccomp(allowed_syscalls []int, allowed_not_kill_syscalls []int, isolated bool) error {
	ctx, err := sg.NewFilter(sg.ActKillProcess)
	if err != nil {
		return err
	}

	reader, writer, err := os.Pipe()
	if err != nil {
		return err
	}
	defer reader.Close()
	defer writer.Close()

	for _, syscall := range allowed_syscalls {
		ctx.AddRule(sg.ScmpSyscall(syscall), sg.ActAllow)
	}

	for _, syscall := range allowed_not_kill_syscalls {
		ctx.AddRule(sg.ScmpSyscall(syscall), sg.ActErrno)
	}

	if isolated {
		if err := ctx.AddRule(sg.ScmpSyscall(syscall.SYS_SOCKET), sg.ActErrno.SetReturnCode(int16(syscall.EPERM))); err != nil {
			return err
		}
	}
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
		SYS_SECCOMP,
		uintptr(SeccompSetModeFilter),
		uintptr(SeccompFilterFlagTSYNC),
		uintptr(unsafe.Pointer(&bpf)),
	)

	if err2 != 0 {
		return err2
	}

	return nil
}
