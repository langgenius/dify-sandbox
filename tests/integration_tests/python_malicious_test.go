package integrationtests_test

import (
	"strings"
	"testing"

	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/service"
)

func TestSysFork(t *testing.T) {
	// Test case for sys_fork
	resp := service.RunPython3Code(`
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
	resp := service.RunPython3Code(`
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

func TestRunCommand(t *testing.T) {
	// Test case for run_command
	resp := service.RunPython3Code(`
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

func TestReadEtcPasswd(t *testing.T) {
	resp := service.RunPython3Code(`
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
