// Diagnostic-only filter compatibility shim. Never part of a runtime image or
// public endpoint. The syscall policy is the unchanged production implementation.
package main

import "C"
import (
	"github.com/langgenius/dify-sandbox/internal/core/lib"
	"github.com/langgenius/dify-sandbox/internal/static/nodejs_syscall"
	"github.com/langgenius/dify-sandbox/internal/static/python_syscall"
	sg "github.com/seccomp/libseccomp-golang"
)

// AST equivalence tests bind these names to both unchanged native owners.
var pythonFilesystem = []string{"getcwd", "newfstatat", "readlinkat", "getdents64", "fstat", "lseek", "statfs", "fstatfs", "statx", "epoll_pwait", "faccessat", "faccessat2"}
var nodeFilesystem = []string{"getcwd", "newfstatat", "readlinkat", "getdents64", "fstat", "lseek", "statfs", "fstatfs", "statx", "faccessat", "faccessat2"}

//export CS5Filter
func CS5Filter(language, scoped C.int) C.int {
	var allowed, legacy []int
	var names []string
	switch language {
	case 1:
		allowed = append(allowed, python_syscall.ALLOW_SYSCALLS...)
		legacy = python_syscall.ALLOW_ERROR_SYSCALLS
		names = pythonFilesystem
	case 2:
		allowed = append(allowed, nodejs_syscall.ALLOW_SYSCALLS...)
		legacy = nodejs_syscall.ALLOW_ERROR_SYSCALLS
		names = nodeFilesystem
	default:
		return -1
	}
	if scoped != 0 && scoped != 1 {
		return -1
	}
	for _, name := range names {
		number, err := sg.GetSyscallFromName(name)
		if err != nil || number < 0 {
			return -1
		}
		found := false
		for _, existing := range allowed {
			found = found || existing == int(number)
		}
		if !found {
			allowed = append(allowed, int(number))
		}
	}
	lib.SetNoNewPrivs()
	var err error
	if scoped == 1 {
		err = lib.SeccompIsolatedScoped(allowed, legacy)
	} else {
		err = lib.SeccompIsolated(allowed, legacy)
	}
	if err != nil {
		return -1
	}
	return 0
}
func main() {}
