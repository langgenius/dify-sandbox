//go:build linux

package lib

import (
	"errors"
	"os"
	"syscall"
	"testing"

	sg "github.com/seccomp/libseccomp-golang"
)

type recordedSeccompRule struct {
	syscall    sg.ScmpSyscall
	action     sg.ScmpAction
	conditions []sg.ScmpCondition
}

type fakeSeccompFilter struct {
	rules                 []recordedSeccompRule
	conditionalRules      []recordedSeccompRule
	addRuleErr            error
	addRuleConditionalErr error
	exportErr             error
	exported              bool
}

func (f *fakeSeccompFilter) AddRule(call sg.ScmpSyscall, action sg.ScmpAction) error {
	if f.addRuleErr != nil {
		return f.addRuleErr
	}
	f.rules = append(f.rules, recordedSeccompRule{syscall: call, action: action})
	return nil
}

func (f *fakeSeccompFilter) AddRuleConditional(
	call sg.ScmpSyscall,
	action sg.ScmpAction,
	conditions []sg.ScmpCondition,
) error {
	if f.addRuleConditionalErr != nil {
		return f.addRuleConditionalErr
	}
	f.conditionalRules = append(f.conditionalRules, recordedSeccompRule{
		syscall:    call,
		action:     action,
		conditions: conditions,
	})
	return nil
}

func (f *fakeSeccompFilter) ExportBPF(_ *os.File) error {
	if f.exportErr != nil {
		return f.exportErr
	}
	f.exported = true
	return nil
}

func makeTestCondition(
	arg uint,
	comparison sg.ScmpCompareOp,
	values ...uint64,
) (sg.ScmpCondition, error) {
	condition := sg.ScmpCondition{
		Argument: arg,
		Op:       comparison,
		Operand1: values[0],
	}
	if len(values) > 1 {
		condition.Operand2 = values[1]
	}
	return condition, nil
}

func TestBuildSeccompBPFRejectsExecutableMemory(t *testing.T) {
	filter := &fakeSeccompFilter{}
	output, err := os.CreateTemp(t.TempDir(), "seccomp-bpf")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()

	allowedSyscalls := []int{
		syscall.SYS_MMAP,
		syscall.SYS_READ,
		syscall.SYS_MPROTECT,
	}
	errnoSyscalls := []int{syscall.SYS_WRITE}
	for syscallNumber := range nonOverridableDeniedSyscalls {
		allowedSyscalls = append(allowedSyscalls, syscallNumber)
		errnoSyscalls = append(errnoSyscalls, syscallNumber)
	}

	err = buildSeccompBPF(
		filter,
		output,
		allowedSyscalls,
		errnoSyscalls,
		[]int{syscall.SYS_MMAP, syscall.SYS_MPROTECT, syscall.SYS_MMAP},
		makeTestCondition,
	)
	if err != nil {
		t.Fatalf("buildSeccompBPF() error = %v", err)
	}

	for _, rule := range filter.rules {
		if rule.syscall == sg.ScmpSyscall(syscall.SYS_MMAP) ||
			rule.syscall == sg.ScmpSyscall(syscall.SYS_MPROTECT) {
			t.Fatalf("guarded syscall %d received an unconditional rule", rule.syscall)
		}
	}

	if len(filter.rules) != 2 {
		t.Fatalf("unconditional rule count = %d, want 2", len(filter.rules))
	}
	if filter.rules[0].syscall != sg.ScmpSyscall(syscall.SYS_READ) ||
		filter.rules[0].action != sg.ActAllow {
		t.Fatalf("first unconditional rule = %+v, want read/allow", filter.rules[0])
	}
	if filter.rules[1].syscall != sg.ScmpSyscall(syscall.SYS_WRITE) ||
		filter.rules[1].action != sg.ActErrno {
		t.Fatalf("second unconditional rule = %+v, want write/errno", filter.rules[1])
	}

	if len(filter.conditionalRules) != 2 {
		t.Fatalf("conditional rule count = %d, want 2", len(filter.conditionalRules))
	}
	for index, rule := range filter.conditionalRules {
		wantSyscall := syscall.SYS_MMAP
		if index == 1 {
			wantSyscall = syscall.SYS_MPROTECT
		}
		if rule.syscall != sg.ScmpSyscall(wantSyscall) {
			t.Fatalf("conditional rule %d syscall = %d, want %d", index, rule.syscall, wantSyscall)
		}
		if rule.action != sg.ActAllow {
			t.Fatalf("conditional rule %d action = %v, want ActAllow", index, rule.action)
		}
		if len(rule.conditions) != 1 {
			t.Fatalf("conditional rule %d condition count = %d, want 1", index, len(rule.conditions))
		}

		condition := rule.conditions[0]
		if condition.Argument != 2 {
			t.Fatalf("conditional rule %d argument = %d, want 2", index, condition.Argument)
		}
		if condition.Op != sg.CompareMaskedEqual {
			t.Fatalf(
				"conditional rule %d operator = %v, want CompareMaskedEqual",
				index,
				condition.Op,
			)
		}

		wantMask := uint64(syscall.PROT_EXEC)
		if condition.Operand1 != wantMask || condition.Operand2 != 0 {
			t.Fatalf(
				"conditional rule %d operands = (%d, %d), want (%d, 0)",
				index,
				condition.Operand1,
				condition.Operand2,
				wantMask,
			)
		}
	}

	if !filter.exported {
		t.Fatal("expected configured filter to be exported")
	}
}

func TestBuildSeccompBPFReturnsConstructionErrors(t *testing.T) {
	sentinel := errors.New("sentinel")

	tests := []struct {
		name          string
		filter        *fakeSeccompFilter
		allowed       []int
		errno         []int
		guarded       []int
		makeCondition seccompConditionFactory
	}{
		{
			name:          "allow rule",
			filter:        &fakeSeccompFilter{addRuleErr: sentinel},
			allowed:       []int{syscall.SYS_READ},
			makeCondition: makeTestCondition,
		},
		{
			name:          "errno rule",
			filter:        &fakeSeccompFilter{addRuleErr: sentinel},
			errno:         []int{syscall.SYS_WRITE},
			makeCondition: makeTestCondition,
		},
		{
			name:    "no-exec condition",
			filter:  &fakeSeccompFilter{},
			guarded: []int{syscall.SYS_MMAP},
			makeCondition: func(
				uint,
				sg.ScmpCompareOp,
				...uint64,
			) (sg.ScmpCondition, error) {
				return sg.ScmpCondition{}, sentinel
			},
		},
		{
			name:          "conditional rule",
			filter:        &fakeSeccompFilter{addRuleConditionalErr: sentinel},
			guarded:       []int{syscall.SYS_MMAP},
			makeCondition: makeTestCondition,
		},
		{
			name:          "BPF export",
			filter:        &fakeSeccompFilter{exportErr: sentinel},
			makeCondition: makeTestCondition,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output, err := os.CreateTemp(t.TempDir(), "seccomp-bpf")
			if err != nil {
				t.Fatal(err)
			}
			defer output.Close()

			err = buildSeccompBPF(
				test.filter,
				output,
				test.allowed,
				test.errno,
				test.guarded,
				test.makeCondition,
			)
			if !errors.Is(err, sentinel) {
				t.Fatalf("buildSeccompBPF() error = %v, want sentinel", err)
			}
		})
	}
}
