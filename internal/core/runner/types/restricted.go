package types

import (
	"errors"
	"time"
	"unicode/utf8"
)

// Language is selected by trusted server code after request validation.
type Language string

const (
	Python3 Language = "python3"
	NodeJS  Language = "nodejs"
)

// ExecutionLimits are internal profile values, never caller options. Core dumps
// are disabled (RLIMIT_CORE=0) by the launcher, separately from these limits.
type ExecutionLimits struct {
	BodyBytes, CodeBytes, StdoutBytes, StderrBytes, ResponseBytes int64
	Wall, Cleanup                                                 time.Duration
	AddressSpaceBytes, CPUSeconds, Tasks, OpenFiles, FileBytes    uint64
}

// RestrictedLimits returns a fresh finite server-owned profile by value.
func RestrictedLimits(language Language) (ExecutionLimits, error) {
	limits := ExecutionLimits{BodyBytes: 1048576, CodeBytes: 262144, StdoutBytes: 262144, StderrBytes: 65536, ResponseBytes: 2097152, Wall: 5 * time.Second, Cleanup: time.Second, CPUSeconds: 2, Tasks: 64, OpenFiles: 64, FileBytes: 1048576}
	switch language {
	case Python3:
		limits.AddressSpaceBytes = 1073741824
	case NodeJS:
		limits.AddressSpaceBytes = 1610612736
	default:
		return ExecutionLimits{}, errors.New("unsupported_language")
	}
	return limits, nil
}

type ExecutionOutcome string
type StopReason string
type ProcessState string
type CleanupState string

const (
	OutcomeSucceeded            ExecutionOutcome = "succeeded"
	OutcomeChildFailed          ExecutionOutcome = "child_failed"
	OutcomePolicyBlocked        ExecutionOutcome = "policy_blocked"
	OutcomeResourceExceeded     ExecutionOutcome = "resource_exceeded"
	OutcomeCanceled             ExecutionOutcome = "canceled"
	OutcomeRejected             ExecutionOutcome = "rejected"
	OutcomeInfrastructureFailed ExecutionOutcome = "infrastructure_failed"
	OutcomeUnknown              ExecutionOutcome = "unknown"

	ReasonOK                  StopReason = "ok"
	ReasonInvalidInput        StopReason = "invalid_input"
	ReasonInputLimit          StopReason = "input_limit"
	ReasonCapacityUnavailable StopReason = "capacity_unavailable"
	ReasonServiceUnavailable  StopReason = "service_unavailable"
	ReasonStartFailed         StopReason = "start_failed"
	ReasonChildExit           StopReason = "child_exit"
	ReasonSeccompOrSIGSYS     StopReason = "seccomp_or_sigsys"
	ReasonWallLimit           StopReason = "wall_limit"
	ReasonStdoutLimit         StopReason = "stdout_limit"
	ReasonStderrLimit         StopReason = "stderr_limit"
	ReasonCPULimit            StopReason = "cpu_limit"
	ReasonFileSizeLimit       StopReason = "file_size_limit"
	ReasonCanceled            StopReason = "canceled"
	ReasonIOFailed            StopReason = "io_failed"
	ReasonInvalidOutput       StopReason = "invalid_output"
	ReasonCleanupFailed       StopReason = "cleanup_failed"
	ReasonUnclassifiedSignal  StopReason = "unclassified_signal"
	ReasonExecutionUnknown    StopReason = "execution_unknown"

	ProcessNotStarted ProcessState = "not_started"
	ProcessExited     ProcessState = "exited"
	ProcessSignaled   ProcessState = "signaled"
	ProcessUnknown    ProcessState = "unknown"
	CleanupConfirmed  CleanupState = "confirmed"
	CleanupNotStarted CleanupState = "not_started"
	CleanupUnknown    CleanupState = "unknown"
)

// ExecutionResult contains supervisor facts. Capture's partial result is not a
// finalized result: only RunRestricted can establish root/UID cleanup. Callers
// must copy captured byte slices and the monotonic reason set when finalizing.
// This validation proves internal consistency, never deployment capability.
type ExecutionResult struct {
	Stdout, Stderr                                []byte
	Outcome                                       ExecutionOutcome
	ReasonCode                                    StopReason
	ProcessState                                  ProcessState
	ExitCode                                      *int
	Signal                                        *int
	EnforcementReasons                            []StopReason
	CleanupState                                  CleanupState
	UserCPU, SystemCPU, Elapsed                   time.Duration
	MaxRSSBytes, StdoutReadBytes, StderrReadBytes uint64
}

// ValidateExecutionResult rejects unset, contradictory and partial facts. It
// never repairs a result or infers enforcement from child text or SIGKILL.
func ValidateExecutionResult(r ExecutionResult) error {
	invalid := func() error { return errors.New("invalid_execution_result") }
	if r.UserCPU < 0 || r.SystemCPU < 0 || r.Elapsed < 0 || len(r.Stdout) > 262144 || len(r.Stderr) > 65536 || r.StdoutReadBytes < uint64(len(r.Stdout)) || r.StderrReadBytes < uint64(len(r.Stderr)) {
		return invalid()
	}
	switch r.ProcessState {
	case ProcessNotStarted, ProcessUnknown:
		if r.ProcessState == ProcessNotStarted && (len(r.Stdout) != 0 || len(r.Stderr) != 0 || r.StdoutReadBytes != 0 || r.StderrReadBytes != 0) {
			return invalid()
		}
		if r.ExitCode != nil || r.Signal != nil {
			return invalid()
		}
	case ProcessExited:
		if r.ExitCode == nil || *r.ExitCode < 0 || *r.ExitCode > 255 || r.Signal != nil {
			return invalid()
		}
	case ProcessSignaled:
		if r.Signal == nil || *r.Signal < 1 || *r.Signal > 64 || r.ExitCode != nil {
			return invalid()
		}
	default:
		return invalid()
	}
	switch r.CleanupState {
	case CleanupConfirmed:
		if r.ProcessState != ProcessExited && r.ProcessState != ProcessSignaled {
			return invalid()
		}
	case CleanupNotStarted:
		if r.ProcessState != ProcessNotStarted {
			return invalid()
		}
	case CleanupUnknown:
	default:
		return invalid()
	}
	if len(r.EnforcementReasons) > 8 {
		return invalid()
	}
	for i, reason := range r.EnforcementReasons {
		switch reason {
		case ReasonCanceled, ReasonCPULimit, ReasonFileSizeLimit, ReasonInputLimit, ReasonSeccompOrSIGSYS, ReasonStderrLimit, ReasonStdoutLimit, ReasonWallLimit:
		default:
			return invalid()
		}
		if i > 0 && r.EnforcementReasons[i-1] >= reason {
			return invalid()
		}
	}
	has := func(reason StopReason) bool {
		for _, v := range r.EnforcementReasons {
			if v == reason {
				return true
			}
		}
		return false
	}
	if (r.StdoutReadBytes > 262144 && !has(ReasonStdoutLimit)) || (r.StderrReadBytes > 65536 && !has(ReasonStderrLimit)) {
		return invalid()
	}
	// Real SIGSYS/XCPU/XFSZ are enforcing observations on the supported Linux
	// architectures; a shell exit number is deliberately not a signal.
	if r.ProcessState == ProcessSignaled {
		for _, entry := range []struct {
			signal int
			reason StopReason
		}{{31, ReasonSeccompOrSIGSYS}, {24, ReasonCPULimit}, {25, ReasonFileSizeLimit}} {
			if *r.Signal == entry.signal && !has(entry.reason) {
				return invalid()
			}
		}
	}
	matches := func(outcome ExecutionOutcome, reason StopReason) error {
		if r.Outcome != outcome || r.ReasonCode != reason {
			return invalid()
		}
		return nil
	}
	if r.ProcessState == ProcessUnknown {
		if r.CleanupState != CleanupUnknown {
			return invalid()
		}
		return matches(OutcomeUnknown, ReasonExecutionUnknown)
	}
	if r.CleanupState == CleanupUnknown {
		return matches(OutcomeUnknown, ReasonCleanupFailed)
	}
	if r.ProcessState == ProcessNotStarted {
		for _, reason := range r.EnforcementReasons {
			if reason != ReasonInputLimit && reason != ReasonCanceled {
				return invalid()
			}
		}
	}
	for _, reason := range []StopReason{ReasonSeccompOrSIGSYS, ReasonInputLimit, ReasonWallLimit, ReasonStdoutLimit, ReasonStderrLimit, ReasonCPULimit, ReasonFileSizeLimit, ReasonCanceled} {
		if !has(reason) {
			continue
		}
		switch reason {
		case ReasonSeccompOrSIGSYS:
			return matches(OutcomePolicyBlocked, reason)
		case ReasonCanceled:
			return matches(OutcomeCanceled, reason)
		default:
			return matches(OutcomeResourceExceeded, reason)
		}
	}
	if r.ProcessState == ProcessNotStarted {
		switch r.ReasonCode {
		case ReasonInvalidInput, ReasonCapacityUnavailable, ReasonServiceUnavailable:
			return matches(OutcomeRejected, r.ReasonCode)
		case ReasonStartFailed:
			return matches(OutcomeInfrastructureFailed, ReasonStartFailed)
		default:
			return invalid()
		}
	}
	if r.ProcessState == ProcessSignaled {
		return matches(OutcomeUnknown, ReasonUnclassifiedSignal)
	}
	if r.ReasonCode == ReasonIOFailed || r.ReasonCode == ReasonInvalidOutput {
		return matches(OutcomeInfrastructureFailed, r.ReasonCode)
	}
	if !utf8.Valid(r.Stdout) || !utf8.Valid(r.Stderr) || r.StdoutReadBytes != uint64(len(r.Stdout)) || r.StderrReadBytes != uint64(len(r.Stderr)) {
		return invalid()
	}
	if *r.ExitCode != 0 {
		return matches(OutcomeChildFailed, ReasonChildExit)
	}
	return matches(OutcomeSucceeded, ReasonOK)
}
