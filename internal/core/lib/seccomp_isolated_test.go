//go:build linux

package lib

import (
	sg "github.com/seccomp/libseccomp-golang"
	"os"
	"os/exec"
	"syscall"
	"testing"
)

func TestBuilderSocketReturnsEPERMAndOrdinaryFilterUnchanged(t *testing.T) {
	mode := os.Getenv("CS3_SECCOMP_CHILD")
	if mode != "" {
		target := syscall.SYS_SOCKET
		if mode == "ordinary" {
			target = syscall.SYS_GETPID
		}
		allowed := []int{}
		for n := 0; n <= 450; n++ {
			if n != target {
				allowed = append(allowed, n)
			}
		}
		SetNoNewPrivs()
		var err error
		if mode == "ordinary" {
			err = Seccomp(allowed, []int{target})
		} else {
			err = SeccompIsolated(allowed, nil)
		}
		if err != nil {
			os.Exit(2)
		}
		if mode == "ordinary" {
			value, _, errno := syscall.RawSyscall(uintptr(target), 0, 0, 0)
			if value != 0 || errno != 0 {
				os.Exit(3)
			}
		} else {
			for _, family := range []uintptr{syscall.AF_INET, syscall.AF_INET6, syscall.AF_UNIX} {
				value, _, errno := syscall.RawSyscall(uintptr(target), family, syscall.SOCK_STREAM, 0)
				if value != ^uintptr(0) || errno != syscall.EPERM {
					os.Exit(4)
				}
			}
		}
		os.Exit(0)
	}
	for _, mode := range []string{"builder", "ordinary"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestBuilderSocketReturnsEPERMAndOrdinaryFilterUnchanged$")
		cmd.Env = append(os.Environ(), "CS3_SECCOMP_CHILD="+mode)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s kernel rule failed: %v %s", mode, err, output)
		}
	}
}
func TestBuilderDenialCannotBeOverriddenByAllowList(t *testing.T) {
	number, _ := sg.GetSyscallFromName("socket")
	if err := SeccompIsolated([]int{int(number)}, nil); err == nil {
		t.Fatal("socket allow override accepted")
	}
}
