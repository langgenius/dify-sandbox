package integrationtests_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/service"
)

const pythonMemoryProtectionScript = `
import ctypes
import os

PROT_READ = 1
PROT_WRITE = 2
PROT_EXEC = 4
MAP_PRIVATE = 2
MAP_ANONYMOUS = 0x20
MAP_FAILED = ctypes.c_void_p(-1).value
PAGE_SIZE = 4096

libc = ctypes.CDLL(None, use_errno=True)
libc.mmap.argtypes = [
    ctypes.c_void_p,
    ctypes.c_size_t,
    ctypes.c_int,
    ctypes.c_int,
    ctypes.c_int,
    ctypes.c_long,
]
libc.mmap.restype = ctypes.c_void_p
libc.mprotect.argtypes = [ctypes.c_void_p, ctypes.c_size_t, ctypes.c_int]
libc.mprotect.restype = ctypes.c_int
libc.munmap.argtypes = [ctypes.c_void_p, ctypes.c_size_t]
libc.munmap.restype = ctypes.c_int

address = libc.mmap(
    None,
    PAGE_SIZE,
    %d,
    MAP_PRIVATE | MAP_ANONYMOUS,
    -1,
    0,
)
if address == MAP_FAILED:
    errno = ctypes.get_errno()
    raise OSError(errno, os.strerror(errno))

updated_protection = %d
if updated_protection >= 0 and libc.mprotect(address, PAGE_SIZE, updated_protection) != 0:
    errno = ctypes.get_errno()
    raise OSError(errno, os.strerror(errno))

if libc.munmap(address, PAGE_SIZE) != 0:
    errno = ctypes.get_errno()
    raise OSError(errno, os.strerror(errno))

print("memory-ok")
`

func TestPythonMemoryProtectionRejectsExecutableMemory(t *testing.T) {
	const (
		protRead  = 1
		protWrite = 2
		protExec  = 4
	)

	tests := []struct {
		name              string
		initialProtection int
		updatedProtection int
		wantKilled        bool
	}{
		{
			name:              "mmap RW",
			initialProtection: protRead | protWrite,
			updatedProtection: -1,
		},
		{
			name:              "mmap RX",
			initialProtection: protRead | protExec,
			updatedProtection: -1,
			wantKilled:        true,
		},
		{
			name:              "mmap RWX",
			initialProtection: protRead | protWrite | protExec,
			updatedProtection: -1,
			wantKilled:        true,
		},
		{
			name:              "mmap RW then mprotect RX",
			initialProtection: protRead | protWrite,
			updatedProtection: protRead | protExec,
			wantKilled:        true,
		},
		{
			name:              "mmap RW then mprotect RWX",
			initialProtection: protRead | protWrite,
			updatedProtection: protRead | protWrite | protExec,
			wantKilled:        true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resp := service.RunPython3Code(
				context.TODO(),
				fmt.Sprintf(
					pythonMemoryProtectionScript,
					test.initialProtection,
					test.updatedProtection,
				),
				"",
				&types.RunnerOptions{},
			)
			if resp.Code != 0 {
				t.Fatal(resp)
			}

			data := resp.Data.(*service.RunCodeResponse)
			if test.wantKilled {
				if data.ExitCode == 0 {
					t.Fatalf("expected seccomp termination, got success: %+v", data)
				}
				if !strings.Contains(data.Error, "operation not permitted") {
					t.Fatalf("expected seccomp error, got: %q", data.Error)
				}
				return
			}

			if data.ExitCode != 0 {
				t.Fatalf("expected successful memory operation, got: %+v", data)
			}
			if data.Stderr != "" || data.Error != "" {
				t.Fatalf("unexpected execution error: %+v", data)
			}
			if !strings.Contains(data.Stdout, "memory-ok") {
				t.Fatalf("unexpected output: %q", data.Stdout)
			}
		})
	}
}

func TestSysFork(t *testing.T) {
	// Test case for sys_fork
	resp := service.RunPython3Code(context.TODO(), `
import os
print(os.fork())
print(123)
	`, "", &types.RunnerOptions{
		EnableNetwork: true,
	})

	if resp.Code != 0 {
		t.Error(resp)
	}

	if resp.Data.(*service.RunCodeResponse).Stdout != "0\n123\n" {
		t.Error(resp.Data.(*service.RunCodeResponse).Stderr)
	}
}

func TestExec(t *testing.T) {
	// Test case for exec
	resp := service.RunPython3Code(context.TODO(), `
import os
os.execl("/bin/ls", "ls")
	`, "", &types.RunnerOptions{
		EnableNetwork: true,
	})
	if resp.Code != 0 {
		t.Error(resp)
	}

	if !strings.Contains(resp.Data.(*service.RunCodeResponse).Error, "process exited with code") {
		t.Error(resp.Data.(*service.RunCodeResponse).Error)
	}

	if !strings.Contains(resp.Data.(*service.RunCodeResponse).Error, "operation not permitted") {
		t.Error(resp.Data.(*service.RunCodeResponse).Error)
	}
}

func TestRunCommand(t *testing.T) {
	// Test case for run_command
	resp := service.RunPython3Code(context.TODO(), `
import subprocess
subprocess.run(["ls", "-l"])
	`, "", &types.RunnerOptions{
		EnableNetwork: true,
	})
	if resp.Code != 0 {
		t.Error(resp)
	}

	if !strings.Contains(resp.Data.(*service.RunCodeResponse).Error, "process exited with code") {
		t.Error(resp.Data.(*service.RunCodeResponse).Error)
	}

	if !strings.Contains(resp.Data.(*service.RunCodeResponse).Error, "operation not permitted") {
		t.Error(resp.Data.(*service.RunCodeResponse).Error)
	}
}

func TestReadEtcPasswd(t *testing.T) {
	resp := service.RunPython3Code(context.TODO(), `
print(open("/etc/passwd").read())
	`, "", &types.RunnerOptions{
		EnableNetwork: true,
	})
	if resp.Code != 0 {
		t.Error(resp)
	}

	if !strings.Contains(resp.Data.(*service.RunCodeResponse).Stderr, "No such file or directory") {
		t.Error(resp.Data.(*service.RunCodeResponse).Stderr)
	}

	if !strings.Contains(resp.Data.(*service.RunCodeResponse).Error, resp.Data.(*service.RunCodeResponse).Stderr) {
		t.Error(resp.Data.(*service.RunCodeResponse).Error)
	}
}
