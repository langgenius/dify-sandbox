//go:build linux

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
		// clone and clone3 get their own rules in addThreadRules
		if isCloneSyscall(syscall) {
			continue
		}
		ctx.AddRule(sg.ScmpSyscall(syscall), sg.ActErrno)
	}

	// The Go runtime in the loaded library (and the Node.js thread pool) may
	// start a new OS thread at any time after the filter is installed, so
	// blocking threads makes runs fail at random.
	if err := addThreadRules(ctx, allowed_syscalls); err != nil {
		return err
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

func isCloneSyscall(n int) bool {
	return n == syscall.SYS_CLONE || n == SYS_CLONE3
}

// addThreadRules lets the process create threads, but not new processes.
// seccomp cannot read the flags of clone3 (they are in a struct passed by
// pointer), so clone3 returns ENOSYS and glibc falls back to clone, whose
// flags are in a register. clone is allowed only with CLONE_THREAD; any other
// clone (fork) keeps returning 0 without running, as before. prctl is allowed
// only for PR_SET_NAME, which new threads use to name themselves. A syscall
// that is already allowed (e.g. through ALLOWED_SYSCALLS) is left as it is.
func addThreadRules(ctx *sg.ScmpFilter, allowed_syscalls []int) error {
	allowed := make(map[int]bool, len(allowed_syscalls))
	for _, n := range allowed_syscalls {
		allowed[n] = true
	}

	if !allowed[SYS_CLONE3] {
		err := ctx.AddRule(sg.ScmpSyscall(SYS_CLONE3), sg.ActErrno.SetReturnCode(int16(syscall.ENOSYS)))
		if err != nil {
			return err
		}
	}

	if !allowed[syscall.SYS_CLONE] {
		thread, err := sg.MakeCondition(0, sg.CompareMaskedEqual, syscall.CLONE_THREAD, syscall.CLONE_THREAD)
		if err != nil {
			return err
		}
		not_thread, err := sg.MakeCondition(0, sg.CompareMaskedEqual, syscall.CLONE_THREAD, 0)
		if err != nil {
			return err
		}
		err = ctx.AddRuleConditional(sg.ScmpSyscall(syscall.SYS_CLONE), sg.ActAllow, []sg.ScmpCondition{thread})
		if err != nil {
			return err
		}
		err = ctx.AddRuleConditional(sg.ScmpSyscall(syscall.SYS_CLONE), sg.ActErrno, []sg.ScmpCondition{not_thread})
		if err != nil {
			return err
		}
	}

	// threading.Thread (Python 3.14+) calls prctl(PR_SET_NAME) in each new thread.
	if !allowed[syscall.SYS_PRCTL] {
		set_name, err := sg.MakeCondition(0, sg.CompareEqual, syscall.PR_SET_NAME)
		if err != nil {
			return err
		}
		err = ctx.AddRuleConditional(sg.ScmpSyscall(syscall.SYS_PRCTL), sg.ActAllow, []sg.ScmpCondition{set_name})
		if err != nil {
			return err
		}
	}

	return nil
}
