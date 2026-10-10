package types

import (
	"testing"
	"time"
)

// These tests catch drift from the finite server-owned profile, invalid final
// lifecycle facts and outcome precedence that could manufacture success.
func TestRestrictedLimitsPerLanguage(t *testing.T) {
	py, err := RestrictedLimits(Python3)
	if err != nil {
		t.Fatal(err)
	}
	js, err := RestrictedLimits(NodeJS)
	if err != nil {
		t.Fatal(err)
	}
	if py.AddressSpaceBytes != 1073741824 || js.AddressSpaceBytes != 1610612736 {
		t.Fatalf("wrong finite AS limits: %d %d", py.AddressSpaceBytes, js.AddressSpaceBytes)
	}
	for _, v := range []ExecutionLimits{py, js} {
		if v.BodyBytes != 1048576 || v.CodeBytes != 262144 || v.StdoutBytes != 262144 || v.StderrBytes != 65536 || v.ResponseBytes != 2097152 || v.Wall != 5*time.Second || v.Cleanup != time.Second || v.CPUSeconds != 2 || v.Tasks != 64 || v.OpenFiles != 64 || v.FileBytes != 1048576 {
			t.Fatalf("wrong finite limits: %+v", v)
		}
	}
	for _, language := range []Language{"", "unknown", "Python3", "jinja2"} {
		if _, err := RestrictedLimits(language); err == nil {
			t.Fatalf("accepted %q", language)
		}
	}
	py.StdoutBytes = 1
	again, _ := RestrictedLimits(Python3)
	if again.StdoutBytes != 262144 {
		t.Fatal("limits were mutable globally")
	}
}

func terminal(outcome ExecutionOutcome, reason StopReason, exit int) ExecutionResult {
	return ExecutionResult{Outcome: outcome, ReasonCode: reason, ProcessState: "exited", ExitCode: &exit, CleanupState: "confirmed"}
}
func stopped(outcome ExecutionOutcome, reason StopReason, signal int, reasons ...StopReason) ExecutionResult {
	return ExecutionResult{Outcome: outcome, ReasonCode: reason, ProcessState: "signaled", Signal: &signal, CleanupState: "confirmed", EnforcementReasons: reasons}
}
func prelaunch(outcome ExecutionOutcome, reason StopReason, reasons ...StopReason) ExecutionResult {
	return ExecutionResult{Outcome: outcome, ReasonCode: reason, ProcessState: "not_started", CleanupState: "not_started", EnforcementReasons: reasons}
}
func TestRestrictedResultStates(t *testing.T) {
	cases := []ExecutionResult{
		terminal("succeeded", "ok", 0), terminal("child_failed", "child_exit", 1), terminal("child_failed", "child_exit", 255),
		stopped("unknown", "unclassified_signal", 9), stopped("policy_blocked", "seccomp_or_sigsys", 31, "seccomp_or_sigsys"),
		stopped("resource_exceeded", "cpu_limit", 24, "cpu_limit"), stopped("resource_exceeded", "file_size_limit", 25, "file_size_limit"),
		prelaunch("rejected", "invalid_input"), prelaunch("rejected", "capacity_unavailable"), prelaunch("rejected", "service_unavailable"), prelaunch("infrastructure_failed", "start_failed"),
		terminal("infrastructure_failed", "io_failed", 0), terminal("infrastructure_failed", "invalid_output", 0),
		prelaunch("resource_exceeded", "input_limit", "input_limit"), prelaunch("canceled", "canceled", "canceled"),
		{Outcome: "unknown", ReasonCode: "execution_unknown", ProcessState: "unknown", CleanupState: "unknown"},
		{Outcome: "unknown", ReasonCode: "cleanup_failed", ProcessState: "not_started", CleanupState: "unknown"},
	}
	for _, reason := range []StopReason{"input_limit", "wall_limit", "stdout_limit", "stderr_limit", "cpu_limit", "file_size_limit"} {
		r := terminal("resource_exceeded", reason, 0)
		r.EnforcementReasons = []StopReason{reason}
		cases = append(cases, r)
	}
	canceled := terminal("canceled", "canceled", 0)
	canceled.EnforcementReasons = []StopReason{"canceled"}
	cases = append(cases, canceled)
	for i, r := range cases {
		if err := ValidateExecutionResult(r); err != nil {
			t.Errorf("row %d %+v: %v", i, r, err)
		}
	}
	shellExit := terminal("child_failed", "child_exit", 159)
	if err := ValidateExecutionResult(shellExit); err != nil {
		t.Fatal("shell-style code must remain normal exit:", err)
	}
}
func TestRestrictedResultRejectsUnsetAndContradictoryFields(t *testing.T) {
	valid := terminal("succeeded", "ok", 0)
	cases := []ExecutionResult{{}}
	mutate := func(f func(*ExecutionResult)) { r := valid; f(&r); cases = append(cases, r) }
	mutate(func(r *ExecutionResult) { r.Outcome = "" })
	mutate(func(r *ExecutionResult) { r.Outcome = "future" })
	mutate(func(r *ExecutionResult) { r.ReasonCode = "" })
	mutate(func(r *ExecutionResult) { r.ReasonCode = "future" })
	mutate(func(r *ExecutionResult) { r.ProcessState = "" })
	mutate(func(r *ExecutionResult) { r.ProcessState = "future" })
	mutate(func(r *ExecutionResult) { r.CleanupState = "" })
	mutate(func(r *ExecutionResult) { r.CleanupState = "future" })
	mutate(func(r *ExecutionResult) { r.ExitCode = nil })
	mutate(func(r *ExecutionResult) { n := -1; r.ExitCode = &n })
	mutate(func(r *ExecutionResult) { n := 256; r.ExitCode = &n })
	mutate(func(r *ExecutionResult) { n := 9; r.Signal = &n })
	mutate(func(r *ExecutionResult) { r.CleanupState = "not_started" })
	mutate(func(r *ExecutionResult) { r.CleanupState = "unknown" })
	mutate(func(r *ExecutionResult) { r.ReasonCode = "child_exit" })
	mutate(func(r *ExecutionResult) { r.Stdout = []byte{0xff} })
	mutate(func(r *ExecutionResult) { r.Stderr = []byte{0xff} })
	mutate(func(r *ExecutionResult) { r.Stdout = make([]byte, 262145) })
	mutate(func(r *ExecutionResult) { r.Stderr = make([]byte, 65537) })
	mutate(func(r *ExecutionResult) { r.StdoutReadBytes = 262145 })
	mutate(func(r *ExecutionResult) { r.StderrReadBytes = 65537 })
	mutate(func(r *ExecutionResult) { r.Stdout = []byte("x") })
	mutate(func(r *ExecutionResult) { r.UserCPU = -1 })
	mutate(func(r *ExecutionResult) { r.SystemCPU = -1 })
	mutate(func(r *ExecutionResult) { r.Elapsed = -1 })
	cases = append(cases, stopped("unknown", "unclassified_signal", 0), stopped("unknown", "unclassified_signal", 65), stopped("unknown", "unclassified_signal", 31), stopped("unknown", "unclassified_signal", 24), stopped("unknown", "unclassified_signal", 25))
	r := prelaunch("rejected", "invalid_input")
	n := 0
	r.ExitCode = &n
	cases = append(cases, r)
	r = prelaunch("rejected", "invalid_input")
	r.CleanupState = "confirmed"
	cases = append(cases, r)
	r = terminal("unknown", "execution_unknown", 0)
	r.CleanupState = "unknown"
	cases = append(cases, r)
	r = terminal("unknown", "cleanup_failed", 0)
	cases = append(cases, r)
	r = terminal("rejected", "invalid_input", 0)
	cases = append(cases, r)
	r = prelaunch("succeeded", "ok")
	cases = append(cases, r)
	r = prelaunch("resource_exceeded", "wall_limit", "wall_limit")
	cases = append(cases, r)
	r = prelaunch("unknown", "cleanup_failed")
	r.CleanupState = "unknown"
	r.Stdout = []byte("x")
	r.StdoutReadBytes = 1
	cases = append(cases, r)
	r = terminal("child_failed", "child_exit", 1)
	r.Stdout = []byte{0xff}
	r.StdoutReadBytes = 1
	cases = append(cases, r)
	r = terminal("infrastructure_failed", "io_failed", 1)
	r.StdoutReadBytes = 262145
	cases = append(cases, r)
	r = terminal("child_failed", "child_exit", 1)
	r.StdoutReadBytes = 1
	cases = append(cases, r)
	for i, r := range cases {
		if err := ValidateExecutionResult(r); err == nil {
			t.Errorf("accepted contradiction %d %+v", i, r)
		}
	}
	exact := valid
	exact.Stdout = make([]byte, 262144)
	exact.Stderr = make([]byte, 65536)
	exact.StdoutReadBytes = 262144
	exact.StderrReadBytes = 65536
	if err := ValidateExecutionResult(exact); err != nil {
		t.Fatal("rejected exact bounds:", err)
	}
}
func TestRestrictedReasonSetBounds(t *testing.T) {
	all := []StopReason{"canceled", "cpu_limit", "file_size_limit", "input_limit", "seccomp_or_sigsys", "stderr_limit", "stdout_limit", "wall_limit"}
	r := terminal("policy_blocked", "seccomp_or_sigsys", 0)
	r.EnforcementReasons = all
	if err := ValidateExecutionResult(r); err != nil {
		t.Fatal(err)
	}
	for _, set := range [][]StopReason{{""}, {"ok"}, {"execution_unknown"}, {"future"}, {"wall_limit", "cpu_limit"}, {"cpu_limit", "cpu_limit"}, append(append([]StopReason{}, all...), "wall_limit")} {
		r.EnforcementReasons = set
		if err := ValidateExecutionResult(r); err == nil {
			t.Fatalf("accepted invalid set %v", set)
		}
	}
}
func TestRestrictedOutcomePrecedence(t *testing.T) {
	set := []StopReason{"canceled", "cpu_limit", "file_size_limit", "input_limit", "seccomp_or_sigsys", "stderr_limit", "stdout_limit", "wall_limit"}
	for _, row := range []struct {
		reason  StopReason
		outcome ExecutionOutcome
	}{{"seccomp_or_sigsys", "policy_blocked"}, {"input_limit", "resource_exceeded"}, {"wall_limit", "resource_exceeded"}, {"stdout_limit", "resource_exceeded"}, {"stderr_limit", "resource_exceeded"}, {"cpu_limit", "resource_exceeded"}, {"file_size_limit", "resource_exceeded"}, {"canceled", "canceled"}} {
		r := terminal(row.outcome, row.reason, 7)
		r.EnforcementReasons = append([]StopReason{}, set...)
		if err := ValidateExecutionResult(r); err != nil {
			t.Fatal(row.reason, err)
		}
		wrong := r
		wrong.Outcome = "child_failed"
		wrong.ReasonCode = "child_exit"
		if err := ValidateExecutionResult(wrong); err == nil {
			t.Fatal("child exit erased enforcement")
		}
		wrong = r
		wrong.Outcome = "infrastructure_failed"
		wrong.ReasonCode = "invalid_output"
		if err := ValidateExecutionResult(wrong); err == nil {
			t.Fatal("output error erased enforcement")
		}
		r.CleanupState = "unknown"
		r.Outcome = "unknown"
		r.ReasonCode = "cleanup_failed"
		if err := ValidateExecutionResult(r); err != nil {
			t.Fatal(err)
		}
		r.ProcessState = "unknown"
		r.ExitCode = nil
		r.ReasonCode = "execution_unknown"
		if err := ValidateExecutionResult(r); err != nil {
			t.Fatal(err)
		}
		for i, v := range set {
			if v == row.reason {
				set = append(set[:i], set[i+1:]...)
				break
			}
		}
	}
}
