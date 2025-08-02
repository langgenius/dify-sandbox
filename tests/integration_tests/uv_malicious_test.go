package integrationtests_test

import (
	"strings"
	"testing"

	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/service"
)

func TestUvSysFork(t *testing.T) {
	// Test case for sys_fork
	resp := service.RunUvCode(`
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

func TestUvExec(t *testing.T) {
	// Test case for exec
	resp := service.RunUvCode(`
import os
os.execl("/bin/ls", "ls")
	`, "", &types.RunnerOptions{
		EnableNetwork: true,
	})
	if resp.Code != 0 {
		t.Error(resp)
	}

	if !strings.Contains(resp.Data.(*service.RunCodeResponse).Stderr, "operation not permitted") {
		t.Error(resp.Data.(*service.RunCodeResponse).Stderr)
	}
}

func TestUvRunCommand(t *testing.T) {
	// Test case for run_command
	resp := service.RunUvCode(`
import subprocess
subprocess.run(["ls", "-l"])
	`, "", &types.RunnerOptions{
		EnableNetwork: true,
	})
	if resp.Code != 0 {
		t.Error(resp)
	}

	if !strings.Contains(resp.Data.(*service.RunCodeResponse).Stderr, "operation not permitted") {
		t.Error(resp.Data.(*service.RunCodeResponse).Stderr)
	}
}

func TestUvReadEtcPasswd(t *testing.T) {
	resp := service.RunUvCode(`
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
}
