package integrationtests_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/service"
)

const nodejsMemoryProtectionScript = `
const koffi = require("koffi");
const libc = koffi.load(null);
const mmap = libc.func("void *mmap(void *, size_t, int, int, int, int64)");
const mprotect = libc.func("int mprotect(void *, size_t, int)");
const munmap = libc.func("int munmap(void *, size_t)");

const pageSize = 4096;
const mapPrivate = 2;
const mapAnonymous = 0x20;
const address = mmap(null, pageSize, %d, mapPrivate | mapAnonymous, -1, 0);
const updatedProtection = %d;

if (updatedProtection >= 0 && mprotect(address, pageSize, updatedProtection) !== 0) {
	throw new Error("mprotect failed");
}
if (munmap(address, pageSize) !== 0) {
	throw new Error("munmap failed");
}

console.log("memory-ok");
`

func TestNodejsMemoryProtectionRejectsExecutableMemory(t *testing.T) {
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
			resp := service.RunNodeJsCode(
				context.TODO(),
				fmt.Sprintf(
					nodejsMemoryProtectionScript,
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

			if data.ExitCode != 0 || data.Error != "" {
				t.Fatalf("expected successful memory operation, got: %+v", data)
			}
			assertNoUnexpectedNodejsStderr(t, data.Stderr)
			if !strings.Contains(data.Stdout, "memory-ok") {
				t.Fatalf("unexpected output: %q", data.Stdout)
			}
		})
	}
}

func TestNodejsRunCommand(t *testing.T) {
	// Test case for run_command
	resp := service.RunNodeJsCode(context.TODO(), `
const { spawn } = require( 'child_process' );
const ls = spawn( 'ls', [ '-lh', '/usr' ] );

ls.stdout.on( 'data', ( data ) => {
    console.log(data);
} );

ls.stderr.on( 'data', ( data ) => {
    console.log(data);
} );

ls.on( 'close', ( code ) => {
    console.log(code);
} );
	`, "", &types.RunnerOptions{})
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

func TestNodejsRunRedeclareFunctionCommand(t *testing.T) {
	// Test case for run_command
	resp := service.RunNodeJsCode(context.TODO(), `
var data;
function main()
{
   return {result: data};
}
function parseInt()
{
   const {execSync} = require('child_process');
   data = execSync("ls -la /", {encoding: "utf8"});
   return 0;
}
console.log(main());
	`, "", &types.RunnerOptions{})
	if resp.Code != 0 {
		t.Error(resp)
	}

	// parseInt should not be executed as it has been fixed
	if !strings.Contains(resp.Data.(*service.RunCodeResponse).Stdout, "result: undefined") {
		t.Error(resp.Data.(*service.RunCodeResponse).Stdout)
	}
}
