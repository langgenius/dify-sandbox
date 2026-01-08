package lib

import (
	"bytes"
	"encoding/binary"
	"os"
	"syscall"
	"unsafe"

	sg "github.com/seccomp/libseccomp-golang"
)

func Seccomp(allowed_syscalls []int, allowed_not_kill_syscalls []int, allowed_syscall_values map[int]uint64) error {
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

	for _, sc := range allowed_syscalls {
		val, ok := allowed_syscall_values[sc]
		if ok {
			ctx.AddRuleConditional(sg.ScmpSyscall(sc), sg.ActAllow, []sg.ScmpCondition{
				{Argument: 0, Op: sg.CompareEqual, Operand1: val},
			})
		} else {
			ctx.AddRule(sg.ScmpSyscall(sc), sg.ActAllow)
		}
	}

	for _, sc := range allowed_not_kill_syscalls {
		switch sc {
		case SYS_CLONE3:
			// use ENOSYS for CLONE3, see: https://github.com/moby/moby/issues/42680
			ctx.AddRule(sg.ScmpSyscall(sc), sg.ActErrno.SetReturnCode(int16(syscall.ENOSYS)))
		default:
			ctx.AddRule(sg.ScmpSyscall(sc), sg.ActErrno)
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
