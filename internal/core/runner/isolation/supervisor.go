package isolation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

type ReservationResolver func(context.Context, InvocationSpec) (Budget, error)
type LaunchFactory func(InvocationSpec, Payload) (Launch, error)
type Supervisor struct {
	profile   Profile
	reserve   ReservationResolver
	launch    LaunchFactory
	admission *admission
	registry  *registry
	intake    *admission
	observer  Observer
}

func NewSupervisor(p Profile, reserve ReservationResolver, launch LaunchFactory, observers ...Observer) (*Supervisor, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	for _, root := range []*string{&p.EnvironmentRoot, &p.WorkRoot, &p.CgroupRoot} {
		resolved, err := filepath.EvalSymlinks(*root)
		if err != nil {
			return nil, err
		}
		*root = resolved
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if reserve == nil || launch == nil {
		return nil, fmt.Errorf("trusted reservation resolver and native launch factory required")
	}
	if err := filepath.WalkDir(p.EnvironmentRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&(os.ModeSocket|os.ModeNamedPipe|os.ModeDevice|os.ModeCharDevice) != 0 {
			return fmt.Errorf("environment contains a service socket, pipe or device: %s", path)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	p.Languages = append([]string(nil), p.Languages...)
	if len(observers) > 1 {
		return nil, fmt.Errorf("at most one trusted observer")
	}
	var observer Observer
	if len(observers) == 1 {
		observer = observers[0]
	}
	return &Supervisor{p, reserve, launch, newAdmission(p.MaxConcurrent, p.MaxQueueDepth, p.MaxQueuedBytes), newRegistry(p.MaxExecutions), newAdmission(p.MaxConcurrent, p.MaxQueueDepth, p.MaxQueuedBytes), observer}, nil
}
func newRequestRoot(base string) (string, error) { return os.MkdirTemp(base, "request-") }

type Execution struct {
	WorkDir     string
	spec        InvocationSpec
	profile     Profile
	cmd         *exec.Cmd
	gate        *os.File
	cgroup      string
	done        chan struct{}
	result      ExecutionResult
	release     func()
	finishOnce  sync.Once
	observer    Observer
	observation Observation
}

func (s *Supervisor) Start(ctx context.Context, spec InvocationSpec, input io.Reader) (*Execution, error) {
	// Validate numeric bounds before using any value for allocation or timers.
	if err := spec.Budget.Validate(); err != nil {
		return nil, err
	}
	if !spec.Budget.within(s.profile.Limits) {
		return nil, fmt.Errorf("budget exceeds profile")
	}
	reservation, err := s.reserve(ctx, spec)
	if err != nil {
		return nil, err
	}
	if owned, err := s.registry.lookup(spec.ExecutionID, spec.InvocationDigest); err != nil || owned != nil {
		return owned, err
	}
	release, err := s.admission.acquire(ctx, spec.Budget.InputBytes, time.Duration(spec.Budget.QueueMillis)*time.Millisecond)
	if err != nil {
		return nil, err
	}
	payload, err := readBounded(input, spec.Budget.InputBytes)
	if err != nil {
		release()
		return nil, err
	}
	if err = s.profile.ValidateInvocation(spec, payload, reservation); err != nil {
		release()
		return nil, err
	}
	e := &Execution{spec: spec, profile: s.profile, done: make(chan struct{}), release: release, observer: s.observer, result: ExecutionResult{ExecutionID: spec.ExecutionID, InvocationDigest: spec.InvocationDigest, ExitCode: -1, CleanupState: "clean"}}
	owned, created, err := s.registry.register(spec.ExecutionID, spec.InvocationDigest, e)
	if err != nil || !created {
		release()
		return owned, err
	}
	var parsed Payload
	if err = decodeStrict(payload, &parsed); err != nil {
		e.complete("failed", "input_limit")
		return e, nil
	}
	launch, err := s.launch(spec, parsed)
	if err != nil {
		e.complete("configuration_required", "environment_mismatch")
		return e, nil
	}
	if !filepath.IsAbs(launch.Executable) || launch.BootstrapPath != "/work/bootstrap" {
		e.complete("configuration_required", "environment_mismatch")
		return e, nil
	}
	go e.run(ctx, launch, parsed)
	return e, nil
}
func (e *Execution) ChildEnv() []string {
	return []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/work", "TMPDIR=/work/tmp", "LANG=C.UTF-8", "PYTHONDONTWRITEBYTECODE=1"}
}
func (e *Execution) Wait(ctx context.Context) (ExecutionResult, error) {
	select {
	case <-e.done:
		return e.result, nil
	case <-ctx.Done():
		return ExecutionResult{}, ctx.Err()
	}
}
func (e *Execution) complete(status, reason string) {
	e.finishOnce.Do(func() {
		e.result.Status = status
		e.result.FailureReason = reason
		e.observeCompletion()
		if e.release != nil && e.result.CleanupState == "clean" {
			e.release()
		}
		close(e.done)
	})
}
func (e *Execution) FailBeforeStart(cause error) error {
	if e.gate != nil {
		e.gate.Close()
	}
	if e.cmd != nil && e.cmd.Process != nil {
		e.cmd.Process.Kill()
		e.cmd.Wait()
	}
	if err := e.cleanup(); err != nil {
		e.result.CleanupState = "quarantined"
		e.complete("configuration_required", "cleanup_failed")
	} else {
		e.complete("configuration_required", "environment_mismatch")
	}
	return cause
}
func (e *Execution) ReleaseStartGate() error {
	_, err := e.gate.Write([]byte{1})
	closeErr := e.gate.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func (e *Execution) run(ctx context.Context, launch Launch, payload Payload) {
	start := time.Now()
	var err error
	e.WorkDir, err = newRequestRoot(e.profile.WorkRoot)
	if err != nil {
		e.FailBeforeStart(err)
		return
	}
	gateRead, gateWrite, err := os.Pipe()
	if err != nil {
		e.FailBeforeStart(err)
		return
	}
	e.gate = gateWrite
	defer gateRead.Close()
	config := workerConfig{Spec: e.spec, EnvironmentRoot: e.profile.EnvironmentRoot, Root: e.WorkDir, Launch: launch, Payload: payload, Env: e.ChildEnv()}
	encoded, err := json.Marshal(config)
	if err != nil {
		e.FailBeforeStart(err)
		return
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(e.spec.Budget.WallMillis)*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(runCtx, e.profile.WorkerPath)
	e.cmd = cmd
	cmd.Dir = e.WorkDir
	cmd.Env = e.ChildEnv()
	cmd.ExtraFiles = []*os.File{gateRead}
	cmd.Stdin = bytes.NewReader(encoded)
	setNamespaces(cmd)
	stdout := &limitedBuffer{limit: e.spec.Budget.StdoutBytes}
	stderr := &limitedBuffer{limit: e.spec.Budget.StderrBytes}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = time.Duration(e.spec.Budget.CleanupMillis) * time.Millisecond
	if err = cmd.Start(); err != nil {
		e.FailBeforeStart(err)
		return
	}
	gateRead.Close()
	if err = e.AttachCgroup(cmd.Process.Pid); err != nil {
		e.FailBeforeStart(err)
		return
	}
	if err = e.observeStart(); err != nil {
		e.FailBeforeStart(err)
		return
	}
	if err = e.ReleaseStartGate(); err != nil {
		e.FailBeforeStart(err)
		return
	}
	monitorDone := make(chan struct{})
	cpuExceeded := make(chan struct{}, 1)
	monitorStopped := make(chan struct{})
	go e.monitorCPU(monitorDone, monitorStopped, cpuExceeded)
	err = cmd.Wait()
	close(monitorDone)
	<-monitorStopped
	e.result.ExitCode = cmd.ProcessState.ExitCode()
	e.result.Stdout = stdout.String()
	e.result.Stderr = stderr.String()
	e.result.Usage = e.readUsage()
	e.observeEvents()
	e.result.Usage.WallMillis = time.Since(start).Milliseconds()
	status, reason := "succeeded", ""
	if err != nil {
		status = "failed"
	}
	if e.result.ExitCode == 125 {
		status = "configuration_required"
		reason = "environment_mismatch"
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		status = "failed"
		reason = "deadline"
	} else if runCtx.Err() != nil {
		status = "cancelled"
		reason = ""
	}
	select {
	case <-cpuExceeded:
		status = "failed"
		reason = "cpu_limit"
	default:
	}
	if stdout.exceeded {
		status = "failed"
		reason = "stdout_limit"
	}
	if stderr.exceeded {
		status = "failed"
		reason = "stderr_limit"
	}
	if cleanupErr := e.cleanup(); cleanupErr != nil {
		status = "unknown"
		reason = "cleanup_failed"
		e.result.CleanupState = "quarantined"
	}
	e.complete(status, reason)
}

// Drain excess output without retaining it. CS2 adds prompt termination and
// richer per-stream cancellation; excess can never become successful output.
type limitedBuffer struct {
	data     bytes.Buffer
	limit    int64
	exceeded bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := b.limit - int64(b.data.Len())
	if int64(n) > remaining {
		b.exceeded = true
		p = p[:remaining]
	}
	b.data.Write(p)
	return n, nil
}
func (b *limitedBuffer) String() string { return b.data.String() }

// StartRequest reserves finite decoding capacity before buffering JSON. This
// admission is independent of the worker queue, so slow/oversized bodies cannot
// create unbounded retained request memory. HTTP callers must set a read timeout.
func (s *Supervisor) StartRequest(ctx context.Context, input io.Reader) (*Execution, error) {
	release, err := s.intake.acquire(ctx, s.profile.Limits.InputBytes, time.Duration(s.profile.Limits.QueueMillis)*time.Millisecond)
	if err != nil {
		return nil, err
	}
	defer release()
	req, err := DecodeInvocation(input, s.profile.Limits.InputBytes)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(Payload{Code: req.Code, Preload: req.Preload})
	if err != nil {
		return nil, err
	}
	return s.Start(ctx, req.Options, bytes.NewReader(payload))
}

// Observer is a trusted, constructor-injected diagnostic sink. Observations are
// never serialized into the public result; conformance mounts them separately.
type Observer func(Observation)
type Observation struct {
	ExecutionID   string            `json:"execution_id"`
	Phase         string            `json:"phase"`
	Root          string            `json:"root"`
	Cgroup        string            `json:"cgroup"`
	Namespaces    map[string]string `json:"namespaces"`
	Controls      map[string]string `json:"controls"`
	RootRemoved   bool              `json:"root_removed"`
	CgroupRemoved bool              `json:"cgroup_removed"`
	Result        ExecutionResult   `json:"result"`
}

func (e *Execution) observeCompletion() {
	if e.observer == nil {
		return
	}
	o := e.observation
	o.ExecutionID = e.spec.ExecutionID
	o.Phase = "completed"
	o.Root = e.WorkDir
	o.Cgroup = e.cgroup
	o.Result = e.result
	_, err := os.Stat(e.WorkDir)
	o.RootRemoved = e.WorkDir == "" || os.IsNotExist(err)
	_, err = os.Stat(e.cgroup)
	o.CgroupRemoved = e.cgroup == "" || os.IsNotExist(err)
	e.observer(o)
}
