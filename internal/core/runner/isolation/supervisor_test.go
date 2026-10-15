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
