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
	return seccomp(allowed_syscalls, allowed_not_kill_syscalls, false, false)
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
	return seccomp(allowed, legacyErrno, true, false)
}

func seccomp(allowed_syscalls []int, allowed_not_kill_syscalls []int, isolated bool, scoped bool) error {
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

	for _, number := range allowed_syscalls {
		if scoped && number == syscall.SYS_SOCKET {
			continue
		}
		ctx.AddRule(sg.ScmpSyscall(number), sg.ActAllow)
	}

	for _, number := range allowed_not_kill_syscalls {
		if scoped && number == syscall.SYS_SOCKET {
			continue
		}
		ctx.AddRule(sg.ScmpSyscall(number), sg.ActErrno)
	}

	if scoped {
		for _, family := range []uint64{syscall.AF_INET, syscall.AF_INET6} {
			for _, protocol := range []uint64{0, syscall.IPPROTO_TCP} {
				domain, err := sg.MakeCondition(0, sg.CompareEqual, family)
				if err != nil {
					return err
				}
				kind, err := sg.MakeCondition(1, sg.CompareMaskedEqual, ^uint64(syscall.SOCK_CLOEXEC|syscall.SOCK_NONBLOCK), syscall.SOCK_STREAM)
				if err != nil {
					return err
				}
				proto, err := sg.MakeCondition(2, sg.CompareEqual, protocol)
				if err != nil {
					return err
				}
				if err = ctx.AddRuleConditional(sg.ScmpSyscall(syscall.SYS_SOCKET), sg.ActAllow, []sg.ScmpCondition{domain, kind, proto}); err != nil {
					return err
				}
			}
		}
	} else if isolated {
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

// SeccompIsolatedScoped grants only fixed TCP client operations. Network
// authorization remains the namespace and gateway, never this syscall filter.
func SeccompIsolatedScoped(allowed, legacyErrno []int) error {
	for _, list := range [][]int{allowed, legacyErrno} {
		for _, number := range list {
			if number == syscall.SYS_SOCKET || number == syscall.SYS_SOCKETPAIR {
				return fmt.Errorf("scoped socket policy cannot be overridden")
			}
		}
	}
	fixed := append([]int(nil), allowed...)
	for _, name := range []string{"connect", "sendto", "recvfrom", "sendmsg", "recvmsg", "getsockopt", "setsockopt", "getsockname", "getpeername", "shutdown", "ppoll"} {
		number, err := sg.GetSyscallFromName(name)
		if err != nil || number < 0 {
			return fmt.Errorf("unsupported scoped client syscall %s", name)
		}
		found := false
		for _, existing := range fixed {
			found = found || existing == int(number)
		}
		if !found {
			fixed = append(fixed, int(number))
		}
	}
	return seccomp(fixed, legacyErrno, true, true)
}
