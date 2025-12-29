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
	// ActKillProcess has issues with TSYNC that may cause spurious process kills
	// ActErrno is more reliable - blocks unexpected syscalls with EPERM
	ctx, err := sg.NewFilter(sg.ActErrno.SetReturnCode(1)) // EPERM
	if err != nil {
		return err
	}

	reader, writer, err := os.Pipe()
	if err != nil {
		return err
	}
	defer reader.Close()
	defer writer.Close()

	for _, syscall_num := range allowed_syscalls {
		// For newer syscalls like clone3 (435), rseq (293), statx (291), and io_uring (425-427),
		// libseccomp 2.6.0 can resolve the name but fails to add the rule.
		// Use raw syscall numbers for these.
		var sc sg.ScmpSyscall = sg.ScmpSyscall(syscall_num)
		var name string
		skip_name_resolution := false

		// These syscalls need to use raw numbers, not name resolution
		switch syscall_num {
		case 435: // clone3
			skip_name_resolution = true
			name = "clone3"
		case 293: // rseq
			skip_name_resolution = true
			name = "rseq"
		case 291: // statx
			skip_name_resolution = true
			name = "statx"
		case 425: // io_uring_setup
			skip_name_resolution = true
			name = "io_uring_setup"
		case 426: // io_uring_enter
			skip_name_resolution = true
			name = "io_uring_enter"
		case 427: // io_uring_register
			skip_name_resolution = true
			name = "io_uring_register"
		case 243: // recvmmsg
			name = "recvmmsg"
		}

		if name != "" && !skip_name_resolution {
			if resolved, err := sg.GetSyscallFromName(name); err == nil {
				sc = resolved
			}
		}

		ctx.AddRule(sc, sg.ActAllow)
	}

	for _, syscall_num := range allowed_not_kill_syscalls {
		ctx.AddRule(sg.ScmpSyscall(syscall_num), sg.ActErrno)
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
