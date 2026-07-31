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
	"golang.org/x/sys/unix"
)

// These syscalls can create executable mappings or bypass the process boundary.
// They remain denied even when an operator includes them in ALLOWED_SYSCALLS.
var nonOverridableDeniedSyscalls = map[int]struct{}{
	unix.SYS_EXECVE:            {},
	unix.SYS_EXECVEAT:          {},
	unix.SYS_MEMFD_CREATE:      {},
	unix.SYS_PKEY_MPROTECT:     {},
	unix.SYS_PROCESS_VM_WRITEV: {},
	unix.SYS_PTRACE:            {},
	unix.SYS_SHMAT:             {},
}

type seccompFilter interface {
	AddRule(sg.ScmpSyscall, sg.ScmpAction) error
	AddRuleConditional(sg.ScmpSyscall, sg.ScmpAction, []sg.ScmpCondition) error
	ExportBPF(*os.File) error
}

type seccompConditionFactory func(uint, sg.ScmpCompareOp, ...uint64) (sg.ScmpCondition, error)

func Seccomp(
	allowedSyscalls []int,
	errnoSyscalls []int,
	execGuardedSyscalls []int,
) error {
	ctx, err := sg.NewFilter(sg.ActKillProcess)
	if err != nil {
		return err
	}
	defer ctx.Release()

	reader, writer, err := os.Pipe()
	if err != nil {
		return err
	}
	defer reader.Close()
	defer writer.Close()

	err = buildSeccompBPF(
		ctx,
		writer,
		allowedSyscalls,
		errnoSyscalls,
		execGuardedSyscalls,
		sg.MakeCondition,
	)
	if err != nil {
		return err
	}

	// read from pipe
	data := make([]byte, 4096)
	n, err := reader.Read(data)
	if err != nil {
		return err
	}

	// load bpf
	sockFilters := make([]syscall.SockFilter, n/8)
	bytesBuffer := bytes.NewBuffer(data[:n])
	err = binary.Read(bytesBuffer, binary.LittleEndian, &sockFilters)
	if err != nil {
		return err
	}

	bpf := syscall.SockFprog{
		Len:    uint16(len(sockFilters)),
		Filter: &sockFilters[0],
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

func buildSeccompBPF(
	ctx seccompFilter,
	output *os.File,
	allowedSyscalls []int,
	errnoSyscalls []int,
	execGuardedSyscalls []int,
	makeCondition seccompConditionFactory,
) error {
	guardedSyscalls := make(map[int]struct{}, len(execGuardedSyscalls))
	uniqueGuardedSyscalls := make([]int, 0, len(execGuardedSyscalls))
	for _, syscallNumber := range execGuardedSyscalls {
		if _, exists := guardedSyscalls[syscallNumber]; exists {
			continue
		}
		guardedSyscalls[syscallNumber] = struct{}{}
		uniqueGuardedSyscalls = append(uniqueGuardedSyscalls, syscallNumber)
	}

	for _, syscallNumber := range allowedSyscalls {
		if _, denied := nonOverridableDeniedSyscalls[syscallNumber]; denied {
			continue
		}
		if _, guarded := guardedSyscalls[syscallNumber]; guarded {
			continue
		}
		if err := ctx.AddRule(sg.ScmpSyscall(syscallNumber), sg.ActAllow); err != nil {
			return fmt.Errorf("add allow rule for syscall %d: %w", syscallNumber, err)
		}
	}

	for _, syscallNumber := range errnoSyscalls {
		if _, denied := nonOverridableDeniedSyscalls[syscallNumber]; denied {
			continue
		}
		if _, guarded := guardedSyscalls[syscallNumber]; guarded {
			continue
		}
		if err := ctx.AddRule(sg.ScmpSyscall(syscallNumber), sg.ActErrno); err != nil {
			return fmt.Errorf("add errno rule for syscall %d: %w", syscallNumber, err)
		}
	}

	if len(uniqueGuardedSyscalls) > 0 {
		noExec, err := makeCondition(
			2,
			sg.CompareMaskedEqual,
			uint64(syscall.PROT_EXEC),
			0,
		)
		if err != nil {
			return fmt.Errorf("create PROT_EXEC seccomp condition: %w", err)
		}

		for _, syscallNumber := range uniqueGuardedSyscalls {
			// Any request containing PROT_EXEC falls through to ActKillProcess.
			if err := ctx.AddRuleConditional(
				sg.ScmpSyscall(syscallNumber),
				sg.ActAllow,
				[]sg.ScmpCondition{noExec},
			); err != nil {
				return fmt.Errorf("add no-exec rule for syscall %d: %w", syscallNumber, err)
			}
		}
	}

	err := ctx.ExportBPF(output)
	if err != nil {
		return fmt.Errorf("export seccomp BPF: %w", err)
	}

	return nil
}
