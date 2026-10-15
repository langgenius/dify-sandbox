package isolation

import (
	"fmt"
	"github.com/langgenius/dify-sandbox/internal/core/runner/network"
	"math"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Budget struct {
	InputBytes      int64 `json:"input_bytes"`
	CodeBytes       int64 `json:"code_bytes"`
	StdoutBytes     int64 `json:"stdout_bytes"`
	StderrBytes     int64 `json:"stderr_bytes"`
	DiskBytes       int64 `json:"disk_bytes"`
	MemoryBytes     int64 `json:"memory_bytes"`
	PIDs            int64 `json:"pids"`
	FDs             int64 `json:"fds"`
	Concurrent      int64 `json:"concurrent"`
	QueueDepth      int64 `json:"queue_depth"`
	CPUQuotaMicros  int64 `json:"cpu_quota_micros"`
	CPUPeriodMicros int64 `json:"cpu_period_micros"`
	CPUTimeMillis   int64 `json:"cpu_time_millis"`
	WallMillis      int64 `json:"wall_millis"`
	QueueMillis     int64 `json:"queue_millis"`
	CleanupMillis   int64 `json:"cleanup_millis"`
}

func (b Budget) values() []int64 {
	return []int64{b.InputBytes, b.CodeBytes, b.StdoutBytes, b.StderrBytes, b.DiskBytes, b.MemoryBytes, b.PIDs, b.FDs, b.Concurrent, b.QueueDepth, b.CPUQuotaMicros, b.CPUPeriodMicros, b.CPUTimeMillis, b.WallMillis, b.QueueMillis, b.CleanupMillis}
}
func (b Budget) Validate() error {
	for _, v := range b.values() {
		if v <= 0 || v > math.MaxInt64/int64(time.Millisecond) {
			return fmt.Errorf("budget must be positive and safely representable")
		}
	}
	if b.CPUPeriodMicros < 1000 || b.CPUPeriodMicros > 1000000 || b.CPUQuotaMicros < 1000 {
		return fmt.Errorf("invalid kernel CPU quota/period")
	}
	return nil
}
func (b Budget) within(cap Budget) bool {
	for i, v := range b.values() {
		if v > cap.values()[i] {
			return false
		}
	}
	return b.CPUPeriodMicros == cap.CPUPeriodMicros
}

type Profile struct {
	Digest            string
	EnvironmentDigest string
	EnvironmentRoot   string
	WorkRoot          string
	CgroupRoot        string
	WorkerPath        string
	Languages         []string
	Limits            Budget
	MaxConcurrent     int64
	MaxQueueDepth     int64
	MaxQueuedBytes    int64
	MaxExecutions     int64
}

var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var idPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$`)

func (p Profile) Validate() error {
	if err := p.Limits.Validate(); err != nil {
		return err
	}
	if !digestPattern.MatchString(p.Digest) || !digestPattern.MatchString(p.EnvironmentDigest) {
		return fmt.Errorf("profile and environment digests required")
	}
	for _, v := range []string{p.EnvironmentRoot, p.WorkRoot, p.CgroupRoot, p.WorkerPath} {
		if !filepath.IsAbs(v) || filepath.Clean(v) != v || v == "/" {
			return fmt.Errorf("explicit private absolute paths required")
		}
	}
	for _, a := range []string{p.EnvironmentRoot, p.WorkRoot, p.CgroupRoot} {
		for _, b := range []string{p.EnvironmentRoot, p.WorkRoot, p.CgroupRoot} {
			if a != b && strings.HasPrefix(a, b+string(filepath.Separator)) {
				return fmt.Errorf("isolation roots must not overlap")
			}
		}
	}
	if p.WorkRoot == p.EnvironmentRoot || p.CgroupRoot == p.WorkRoot || p.CgroupRoot == p.EnvironmentRoot {
		return fmt.Errorf("distinct roots required")
	}
	if p.MaxConcurrent <= 0 || p.MaxQueueDepth <= 0 || p.MaxQueuedBytes < p.Limits.InputBytes || p.MaxExecutions <= 0 {
		return fmt.Errorf("finite service ceilings required")
	}
	if len(p.Languages) == 0 {
		return fmt.Errorf("languages required")
	}
	for _, v := range p.Languages {
		if v != "python3" && v != "nodejs" {
			return fmt.Errorf("unsupported language")
		}
	}
	return nil
}
func (p Profile) Supports(scope network.NetworkScope) bool {
	return network.ValidateScope(scope) == nil
}
func (p Profile) ValidateInvocation(s InvocationSpec, payload []byte, reservation Budget) error {
	if s.Version != 1 || !idPattern.MatchString(s.ExecutionID) || s.BudgetReservationRef == "" || len(s.BudgetReservationRef) > 256 {
		return fmt.Errorf("invalid invocation identity")
	}
	if s.EnvironmentDigest != p.EnvironmentDigest {
		return failure("environment_mismatch", "environment digest differs")
	}
	if s.ProfileDigest != p.Digest {
		return fmt.Errorf("profile digest differs")
	}
	found := false
	for _, l := range p.Languages {
		found = found || l == s.Language
	}
	if !found {
		return fmt.Errorf("language/profile mismatch")
	}
	if err := s.Budget.Validate(); err != nil {
		return err
	}
	if err := reservation.Validate(); err != nil {
		return err
	}
	if !s.Budget.within(p.Limits) || !s.Budget.within(reservation) {
		return fmt.Errorf("budget exceeds server profile or remaining reservation")
	}
	if err := network.ValidateScope(s.Network); err != nil {
		return err
	}
	if !digestPattern.MatchString(s.InvocationDigest) || s.InvocationDigest != InvocationDigest(s, payload) {
		return fmt.Errorf("invocation digest mismatch")
	}
	if int64(len(payload)) > s.Budget.InputBytes {
		return failure("input_limit", "payload exceeds budget")
	}
	var input Payload
	if err := decodeStrict(payload, &input); err != nil {
		return err
	}
	if int64(len(input.Code))+int64(len(input.Preload)) > s.Budget.CodeBytes {
		return failure("code_limit", "code exceeds budget")
	}
	return nil
}
