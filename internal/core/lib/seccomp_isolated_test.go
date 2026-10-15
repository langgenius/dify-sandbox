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

func TestScopedClientSocketsAreRestricted(t *testing.T) {
	mode := os.Getenv("CS4_SECCOMP_CHILD")
	if mode != "" {
		allowed := []int{}
		for n := 0; n <= 450; n++ {
			if n != syscall.SYS_SOCKET && n != syscall.SYS_SOCKETPAIR {
				allowed = append(allowed, n)
			}
		}
		SetNoNewPrivs()
		if SeccompIsolatedScoped(allowed, nil) != nil {
			os.Exit(2)
		}
		family, kind := uintptr(syscall.AF_INET), uintptr(syscall.SOCK_STREAM)
		switch mode {
		case "ipv6":
			family = syscall.AF_INET6
		case "unix":
			family = syscall.AF_UNIX
		case "udp":
			kind = syscall.SOCK_DGRAM
		case "raw":
			kind = syscall.SOCK_RAW
		}
		fd, _, errno := syscall.RawSyscall(syscall.SYS_SOCKET, family, kind, 0)
		if errno != 0 {
			os.Exit(3)
		}
		syscall.Close(int(fd))
		os.Exit(0)
	}
	for _, mode := range []string{"ipv4", "ipv6", "unix", "udp", "raw"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestScopedClientSocketsAreRestricted$")
		cmd.Env = append(os.Environ(), "CS4_SECCOMP_CHILD="+mode)
		output, err := cmd.CombinedOutput()
		if mode == "ipv4" || mode == "ipv6" {
			if err != nil {
				t.Fatalf("TCP client socket failed: %s %v %s", mode, err, output)
			}
		} else {
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.Sys().(syscall.WaitStatus).Signal() != syscall.SIGSYS {
				t.Fatalf("forbidden socket not killed: %s %v %s", mode, err, output)
			}
		}
	}
}
