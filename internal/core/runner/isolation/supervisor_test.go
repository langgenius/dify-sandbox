package isolation

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestIndependentRoots(t *testing.T) {
	before, _ := os.Getwd()
	base := t.TempDir()
	const n = 12
	roots := make(chan string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			root, err := newRequestRoot(base)
			if err != nil {
				t.Error(err)
				return
			}
			roots <- root
			if err := os.WriteFile(filepath.Join(root, "private"), []byte(root), 0600); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	close(roots)
	seen := map[string]bool{}
	for root := range roots {
		if seen[root] {
			t.Fatal("shared root")
		}
		seen[root] = true
		b, _ := os.ReadFile(filepath.Join(root, "private"))
		if string(b) != root {
			t.Fatal("cross request write")
		}
	}
	after, _ := os.Getwd()
	if before != after {
		t.Fatal("global cwd changed")
	}
	if len(seen) != n {
		t.Fatal("missing roots")
	}
}
func TestAdmissionBoundedCancellationAndRelease(t *testing.T) {
	a := newAdmission(1, 1, 100)
	release, err := a.acquire(context.Background(), 50, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	queued := make(chan error, 1)
	go func() { _, err := a.acquire(ctx, 50, time.Second); queued <- err }()
	deadline := time.Now().Add(time.Second)
	for a.queuedCount() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if _, err := a.acquire(context.Background(), 1, time.Second); err == nil {
		t.Fatal("queue overflow admitted")
	}
	cancel()
	if <-queued == nil {
		t.Fatal("cancelled queue admitted")
	}
	release()
	release()
	next, err := a.acquire(context.Background(), 100, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	next()
	if _, err := a.acquire(context.Background(), 101, time.Second); err == nil {
		t.Fatal("memory reservation exceeded")
	}
}
func TestRegistryIdempotencyAndConflict(t *testing.T) {
	r := newRegistry(2)
	first := &Execution{}
	got, created, err := r.register("one", "digest", first)
	if err != nil || !created || got != first {
		t.Fatal("first registration failed")
	}
	got, created, err = r.register("one", "digest", &Execution{})
	if err != nil || created || got != first {
		t.Fatal("duplicate dispatched")
	}
	if _, _, err = r.register("one", "different", &Execution{}); err == nil {
		t.Fatal("digest conflict accepted")
	}
	r.register("two", "digest", &Execution{})
	if _, _, err = r.register("three", "digest", &Execution{}); err == nil {
		t.Fatal("unbounded registry")
	}
}
func TestMissingAuthorityFailsBeforeRootAllocation(t *testing.T) {
	p := testProfile(t)
	s, err := NewSupervisor(p, nil, nil)
	if err == nil || s != nil {
		t.Fatal("missing authority accepted")
	}
	entries, _ := os.ReadDir(p.WorkRoot)
	if len(entries) != 0 {
		t.Fatal("allocated root without authority")
	}
}
func TestWorkerSetupFailureNeverSucceeds(t *testing.T) {
	p := testProfile(t)
	p.WorkerPath = "/nonexistent-request-worker"
	resolver := func(context.Context, InvocationSpec) (Budget, error) { return p.Limits, nil }
	factory := func(InvocationSpec, Payload) (Launch, error) {
		return Launch{Executable: "/bin/false", BootstrapPath: "/work/bootstrap"}, nil
	}
	sup, err := NewSupervisor(p, resolver, factory)
	if err != nil {
		t.Fatal(err)
	}
	spec := testSpec(p)
	payload := []byte(`{"code":"marker must never execute","preload":""}`)
	spec.InvocationDigest = InvocationDigest(spec, payload)
	e, err := sup.Start(context.Background(), spec, strings.NewReader(string(payload)))
	if err != nil {
		t.Fatal(err)
	}
	result, err := e.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "configuration_required" || result.CleanupState != "clean" {
		t.Fatalf("unexpected result: %+v", result)
	}
	again, err := sup.Start(context.Background(), spec, strings.NewReader(string(payload)))
	if err != nil || again != e {
		t.Fatal("duplicate allocated execution")
	}
	entries, _ := os.ReadDir(p.WorkRoot)
	if len(entries) != 0 {
		t.Fatal("failed start leaked root")
	}
}
func TestFakeCgroupHierarchyRefused(t *testing.T) {
	p := testProfile(t)
	if err := os.WriteFile(filepath.Join(p.CgroupRoot, "cgroup.controllers"), []byte("cpu memory pids"), 0600); err != nil {
		t.Fatal(err)
	}
	e := &Execution{profile: p, spec: testSpec(p), WorkDir: filepath.Join(p.WorkRoot, "request-one")}
	if err := e.AttachCgroup(os.Getpid()); err == nil {
		t.Fatal("ordinary filesystem accepted as cgroup v2")
	}
}
func TestDuplicateJSONKeysRejected(t *testing.T) {
	var p Payload
	if decodeStrict([]byte(`{"code":"safe","code":"other","preload":""}`), &p) == nil {
		t.Fatal("ambiguous JSON accepted")
	}
}
func TestDuplicateReturnsOwnedExecutionWhileCapacityBusy(t *testing.T) {
	p := testProfile(t)
	payload := []byte(`{"code":"print(1)","preload":""}`)
	spec := testSpec(p)
	spec.InvocationDigest = InvocationDigest(spec, payload)
	sup, err := NewSupervisor(p, func(context.Context, InvocationSpec) (Budget, error) { return p.Limits, nil }, func(InvocationSpec, Payload) (Launch, error) { return Launch{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	owned := &Execution{}
	sup.registry.register(spec.ExecutionID, spec.InvocationDigest, owned)
	release, err := sup.admission.acquire(context.Background(), p.Limits.InputBytes, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	got, err := sup.Start(context.Background(), spec, strings.NewReader(string(payload)))
	if err != nil || got != owned {
		t.Fatalf("replay blocked by capacity: %v", err)
	}
}
func TestCgroupCannotBeNestedInsideEnvironment(t *testing.T) {
	p := testProfile(t)
	p.CgroupRoot = filepath.Join(p.EnvironmentRoot, "cgroup")
	if p.Validate() == nil {
		t.Fatal("host cgroup reachable through execution environment")
	}
}
func TestOutputBufferRetainsOnlyBudgetAndRecordsFailure(t *testing.T) {
	b := &limitedBuffer{limit: 3}
	if n, err := b.Write([]byte("ab")); n != 2 || err != nil {
		t.Fatal("write failed")
	}
	if n, err := b.Write([]byte("cdef")); n != 4 || err != nil {
		t.Fatal("drain failed")
	}
	if b.String() != "abc" || !b.exceeded {
		t.Fatal("output overflow silently accepted")
	}
}
func TestSymlinkRootsCannotExposeCgroupInEnvironment(t *testing.T) {
	p := testProfile(t)
	inside := filepath.Join(p.EnvironmentRoot, "delegated")
	if err := os.Mkdir(inside, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "cgroup")
	if err := os.Symlink(inside, link); err != nil {
		t.Fatal(err)
	}
	p.CgroupRoot = link
	if _, err := NewSupervisor(p, func(context.Context, InvocationSpec) (Budget, error) { return p.Limits, nil }, func(InvocationSpec, Payload) (Launch, error) { return Launch{}, nil }); err == nil {
		t.Fatal("symlink exposed delegated cgroup")
	}
}

func TestEnvironmentRefusesServiceSockets(t *testing.T) {
	p := testProfile(t)
	listener, err := net.Listen("unix", filepath.Join(p.EnvironmentRoot, "service.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if _, err = NewSupervisor(p, func(context.Context, InvocationSpec) (Budget, error) { return p.Limits, nil }, func(InvocationSpec, Payload) (Launch, error) { return Launch{}, nil }); err == nil {
		t.Fatal("service socket exposed to user execution")
	}
}

func TestReplayRequiresFullImmutableInvocationValidation(t *testing.T) {
	p := testProfile(t)
	payload := []byte(`{"code":"print(1)","preload":""}`)
	spec := testSpec(p)
	spec.InvocationDigest = InvocationDigest(spec, payload)
	reservation := p.Limits
	launches := 0
	sup, err := NewSupervisor(p, func(context.Context, InvocationSpec) (Budget, error) { return reservation, nil }, func(InvocationSpec, Payload) (Launch, error) {
		launches++
		return Launch{Executable: "/bin/false", BootstrapPath: "/work/bootstrap"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	owned, err := sup.Start(context.Background(), spec, strings.NewReader(string(payload)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = owned.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		change  func(*InvocationSpec)
		payload string
	}{
		{"code", nil, `{"code":"print(2)","preload":""}`},
		{"preload", nil, `{"code":"print(1)","preload":"print('other')"}`},
		{"schema", func(s *InvocationSpec) { s.Version = 2 }, string(payload)},
		{"language", func(s *InvocationSpec) { s.Language = "nodejs" }, string(payload)},
		{"profile", func(s *InvocationSpec) { s.ProfileDigest = strings.Repeat("c", 64) }, string(payload)},
		{"environment", func(s *InvocationSpec) { s.EnvironmentDigest = strings.Repeat("c", 64) }, string(payload)},
		{"network", func(s *InvocationSpec) { s.Network.Mode = "scoped" }, string(payload)},
		{"reservation identity", func(s *InvocationSpec) { s.BudgetReservationRef = "another-reservation" }, string(payload)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changed := spec
			if tc.change != nil {
				tc.change(&changed)
			}
			if _, err := sup.Start(context.Background(), changed, strings.NewReader(tc.payload)); err == nil {
				t.Fatal("invalid replay accepted")
			}
		})
	}
	reservation.MemoryBytes--
	if _, err = sup.Start(context.Background(), spec, strings.NewReader(string(payload))); err == nil {
		t.Fatal("replay exceeds current trusted reservation")
	}
	if launches != 1 {
		t.Fatalf("replay allocated another native launch: %d", launches)
	}
}

func TestQueuedDuplicatesSharePendingOwnerAndOneReservation(t *testing.T) {
	p := testProfile(t)
	p.MaxQueueDepth = 1
	p.Limits.QueueMillis = 2000
	payload := []byte(`{"code":"print(1)","preload":""}`)
	spec := testSpec(p)
	spec.InvocationDigest = InvocationDigest(spec, payload)
	sup, err := NewSupervisor(p, func(context.Context, InvocationSpec) (Budget, error) { return p.Limits, nil }, func(InvocationSpec, Payload) (Launch, error) {
		t.Error("cancelled queued execution launched")
		return Launch{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	held, err := sup.admission.acquire(context.Background(), p.Limits.InputBytes, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer held()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type started struct {
		e   *Execution
		err error
	}
	first := make(chan started, 1)
	go func() { e, err := sup.Start(ctx, spec, strings.NewReader(string(payload))); first <- started{e, err} }()
	var owner *Execution
	select {
	case got := <-first:
		if got.err != nil {
			t.Fatal(got.err)
		}
		owner = got.e
	case <-time.After(time.Second):
		t.Fatal("first submission did not return its pending owner")
	}
	duplicate, err := sup.Start(context.Background(), spec, strings.NewReader(string(payload)))
	if err != nil || duplicate != owner {
		t.Fatalf("queued duplicate did not share owner: %v", err)
	}
	sup.admission.mu.Lock()
	queued, reserved := sup.admission.queued, sup.admission.bytes
	sup.admission.mu.Unlock()
	if queued != 1 || reserved != 2*p.Limits.InputBytes {
		t.Fatalf("duplicate reserved additional capacity: queued=%d bytes=%d", queued, reserved)
	}
	other := spec
	other.ExecutionID = "other"
	other.InvocationDigest = InvocationDigest(other, payload)
	if _, err = sup.Start(context.Background(), other, strings.NewReader(string(payload))); err == nil {
		t.Fatal("unrelated request exceeded queue bound")
	}
	cancel()
	waitCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	result, err := owner.Wait(waitCtx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "cancelled" || result.CleanupState != "clean" {
		t.Fatalf("queued cancellation not finalized: %+v", result)
	}
	again, err := sup.Start(context.Background(), spec, strings.NewReader(string(payload)))
	if err != nil || again != owner {
		t.Fatal("cancelled owner was reallocated")
	}
	sup.admission.mu.Lock()
	queued, reserved = sup.admission.queued, sup.admission.bytes
	sup.admission.mu.Unlock()
	if queued != 0 || reserved != p.Limits.InputBytes {
		t.Fatalf("queue reservation not released exactly once: %d %d", queued, reserved)
	}
}
func TestQueuedDeadlineFinalizesReplayableOwner(t *testing.T) {
	p := testProfile(t)
	p.Limits.QueueMillis = 25
	payload := []byte(`{"code":"print(1)","preload":""}`)
	spec := testSpec(p)
	spec.InvocationDigest = InvocationDigest(spec, payload)
	sup, err := NewSupervisor(p, func(context.Context, InvocationSpec) (Budget, error) { return p.Limits, nil }, func(InvocationSpec, Payload) (Launch, error) { t.Error("expired queue launched"); return Launch{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	held, err := sup.admission.acquire(context.Background(), p.Limits.InputBytes, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer held()
	owner, err := sup.Start(context.Background(), spec, strings.NewReader(string(payload)))
	if err != nil {
		t.Fatalf("no pending owner: %v", err)
	}
	result, err := owner.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" || result.FailureReason != "queue_full" {
		t.Fatalf("wrong terminal queue outcome: %+v", result)
	}
	again, err := sup.Start(context.Background(), spec, strings.NewReader(string(payload)))
	if err != nil || again != owner {
		t.Fatal("queue timeout lost identity")
	}
}

func TestConcurrentFirstSubmissionsClaimOnePendingMemoryReservation(t *testing.T) {
	p := testProfile(t)
	p.MaxConcurrent = 2
	p.MaxQueueDepth = 1
	p.MaxQueuedBytes = 3 * p.Limits.InputBytes
	p.Limits.QueueMillis = 2000
	payload := []byte(`{"code":"print(1)","preload":""}`)
	spec := testSpec(p)
	spec.InvocationDigest = InvocationDigest(spec, payload)
	sup, err := NewSupervisor(p, func(context.Context, InvocationSpec) (Budget, error) { return p.Limits, nil }, func(InvocationSpec, Payload) (Launch, error) {
		t.Error("cancelled queued execution launched")
		return Launch{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		held, err := sup.admission.acquire(context.Background(), p.Limits.InputBytes, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer held()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gate := make(chan struct{})
	owners := make(chan *Execution, 2)
	errors := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-gate
			e, err := sup.Start(ctx, spec, strings.NewReader(string(payload)))
			owners <- e
			errors <- err
		}()
	}
	close(gate)
	first, second := <-owners, <-owners
	if err := <-errors; err != nil {
		t.Fatal(err)
	}
	if err := <-errors; err != nil {
		t.Fatal(err)
	}
	if first == nil || first != second {
		t.Fatal("simultaneous first requests received different owners")
	}
	sup.admission.mu.Lock()
	queued, reserved := sup.admission.queued, sup.admission.bytes
	sup.admission.mu.Unlock()
	if queued != 1 || reserved != p.MaxQueuedBytes {
		t.Fatalf("wrong pending reservation: %d %d", queued, reserved)
	}
	duplicate, err := sup.Start(context.Background(), spec, strings.NewReader(string(payload)))
	if err != nil || duplicate != first {
		t.Fatal("valid replay rejected when worker memory fully reserved")
	}
	cancel()
	if _, err = first.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestObservationReceivesOnlyRetainedStdout(t *testing.T) {
	var observed strings.Builder
	b := &limitedBuffer{limit: 4, observe: func(p []byte) { observed.Write(p) }}
	b.Write([]byte("abcdef"))
	b.Write([]byte("more"))
	if observed.String() != "abcd" || !b.exceeded {
		t.Fatal("diagnostic sink received output beyond budget")
	}
}
