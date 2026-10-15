package isolation

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestWorkerClosedGateRefusesBeforeNamespaceSetup(t *testing.T) {
	gateRead, gateWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer gateRead.Close()
	gateWrite.Close()
	cmd := exec.Command("/out/request-worker")
	cmd.ExtraFiles = []*os.File{gateRead}
	cmd.Env = []string{}
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("worker accepted a closed gate")
	}
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 125 {
		t.Fatalf("worker did not refuse closed gate: %v %s", err, out)
	}
	if !strings.Contains(string(out), "start gate refused") {
		t.Fatalf("worker reached setup before gate: %s", out)
	}
}
