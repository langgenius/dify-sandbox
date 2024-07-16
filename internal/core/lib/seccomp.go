package lib

import (
	"bytes"
	"encoding/binary"
	"os"
	"syscall"
	"unsafe"

	sg "github.com/seccomp/libseccomp-golang"
)

func Seccomp(allowed_syscalls []int, allowed_not_kill_syscalls []int) error {
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
