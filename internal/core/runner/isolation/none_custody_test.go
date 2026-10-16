package isolation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner/uidpool"
)

// These are component custody tests, not proof that Linux created a user
// namespace. The separately qualified native fixture checks actual mappings,
// live payload capabilities and cross-execution access through the supervisor.
func noneCustodyProfile(t *testing.T) Profile {
	t.Helper()
	p := testProfile(t)
	p.HostUIDMin, p.HostUIDMax = 21000, 21002
	return p
}

func TestNativeNoneProfileRequiresLeaseRange(t *testing.T) {
	p := noneCustodyProfile(t)
	if err := p.Validate(); err != nil {
		t.Fatal("valid none-only profile requires no proxy or gateway:", err)
	}
	for _, bounds := range [][2]int{{0, 0}, {10000, 10002}, {21000, 21000}, {21000, 25097}, {65537, 65538}, {21000, 1<<30 + 1}} {
		bad := p
		bad.HostUIDMin, bad.HostUIDMax = bounds[0], bounds[1]
		if err := bad.Validate(); err == nil {
			t.Errorf("none profile admitted invalid UID range [%d,%d)", bounds[0], bounds[1])
		}
	}
}

func TestNativeNoneSupervisorOwnsPersistentLeaseWithoutGateway(t *testing.T) {
	p := noneCustodyProfile(t)
	s, err := NewSupervisor(p, func(context.Context, InvocationSpec) (Budget, error) { return p.Limits, nil }, func(InvocationSpec, Payload) (Launch, error) { return Launch{}, nil })
	if err != nil {
		t.Fatal("none-only supervisor with no gateway:", err)
	}
	defer s.Close()
	if s.uidPool == nil {
		t.Fatal("none-only supervisor has no persistent UID custody")
	}
	// Acquisitions overlap and a separately opened pool competes for the same
	// real filesystem locks. This catches an accidental in-memory replacement.
	var wg sync.WaitGroup
	leased := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			uid, err := s.uidPool.Acquire(ctx)
			if err != nil {
				t.Error(err)
				return
			}
			leased <- uid
		}()
	}
	wg.Wait()
	close(leased)
	seen := map[int]bool{}
	for uid := range leased {
		if uid < 21000 || uid >= 21002 || seen[uid] {
			t.Fatalf("concurrent lease collision or escaped range: %d", uid)
		}
		seen[uid] = true
		defer s.uidPool.ReleaseChecked(uid)
	}
	if len(seen) != 2 {
		t.Fatal("missing concurrent leases")
	}
	other, err := uidpool.NewPersistentUIDPool(21000, 21002, s.journal.root+".uids")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if uid, err := other.Acquire(ctx); err == nil {
		t.Fatalf("second pool acquired live UID %d", uid)
	}
}

func TestNativeNoneLaunchFailureUsesAndReleasesLease(t *testing.T) {
	p := noneCustodyProfile(t)
	p.WorkerPath = "/nonexistent-request-worker"
	pool, err := uidpool.NewPersistentUIDPool(21000, 21002, filepath.Join(t.TempDir(), "leases"))
	if err != nil {
		t.Fatal(err)
	}
	j, err := openJournal(p)
	if err != nil {
		t.Fatal(err)
	}
	defer j.close()
	e := &Execution{profile: p, spec: testSpec(p), uidPool: pool, journal: j, done: make(chan struct{}), result: ExecutionResult{CleanupState: "clean"}}
	e.spec.InvocationDigest = strings.Repeat("a", 64)
	e.run(context.Background(), Launch{Executable: "/bin/false", BootstrapPath: "/work/bootstrap"}, Payload{})
	r, err := e.Wait(context.Background())
	if err != nil || r.Status != "configuration_required" || r.CleanupState != "clean" {
		t.Fatalf("worker failure control: %+v %v", r, err)
	}
	if e.hostUID < 21000 || e.hostUID >= 21002 {
		t.Fatalf("none launch bypassed lease acquisition: host UID %d", e.hostUID)
	}
	if pool.Len() != 2 {
		t.Fatal("clean worker failure leaked UID")
	}
	if _, err := os.Stat(e.WorkDir); !os.IsNotExist(err) {
		t.Fatal("worker failure left request root", err)
	}
}

func TestNativeNoneReplayDoesNotAcquireSecondLease(t *testing.T) {
	p := noneCustodyProfile(t)
	p.WorkerPath = "/nonexistent-request-worker"
	calls := 0
	factory := func(InvocationSpec, Payload) (Launch, error) {
		calls++
		return Launch{Executable: "/bin/false", BootstrapPath: "/work/bootstrap"}, nil
	}
	resolver := func(context.Context, InvocationSpec) (Budget, error) { return p.Limits, nil }
	s, err := NewSupervisor(p, resolver, factory)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"code":"print(1)","preload":""}`
	spec := testSpec(p)
	spec.InvocationDigest = InvocationDigest(spec, []byte(body))
	first, err := s.Start(context.Background(), spec, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	first.Wait(context.Background())
	replay, err := s.Start(context.Background(), spec, strings.NewReader(body))
	if err != nil || replay != first || calls != 1 {
		t.Fatalf("same identity dispatched twice: %v calls=%d", err, calls)
	}
	if first.hostUID < 21000 || first.hostUID >= 21002 {
		t.Errorf("original none execution had no UID lease: %d", first.hostUID)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = NewSupervisor(p, resolver, factory)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	recovered, err := s.Start(context.Background(), spec, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r, err := recovered.Wait(context.Background())
	if err != nil || r.Status != "unknown" || calls != 1 {
		t.Fatalf("restart replay launched: %+v %v calls=%d", r, err, calls)
	}
}

func TestNativeLeaseCompletionRequiresCleanupAndJournal(t *testing.T) {
	for _, failure := range []string{"retained-resource", "journal-ack", "quarantined", "clean"} {
		t.Run(failure, func(t *testing.T) {
			p := noneCustodyProfile(t)
			j, err := openJournal(p)
			if err != nil {
				t.Fatal(err)
			}
			defer j.close()
			pool, err := uidpool.NewPersistentUIDPool(21000, 21001, j.root+".uids")
			if err != nil {
				t.Fatal(err)
			}
			uid, err := pool.Acquire(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(p.WorkRoot, "request-custody")
			if err := pool.TrackResource(uid, root); err != nil {
				t.Fatal(err)
			}
			if failure == "retained-resource" {
				if err := os.Mkdir(root, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if failure == "journal-ack" {
				if err := os.Rename(j.root, j.root+"-lost"); err != nil {
					t.Fatal(err)
				}
			}
			e := &Execution{profile: p, spec: testSpec(p), WorkDir: root, journal: j, uidPool: pool, hostUID: uid, done: make(chan struct{}), result: ExecutionResult{CleanupState: "clean"}}
			e.spec.InvocationDigest = strings.Repeat("a", 64)
			if failure == "quarantined" {
				e.result.CleanupState = "quarantined"
			}
			e.complete("succeeded", "")
			r, _ := e.Wait(context.Background())
			if failure == "clean" {
				if r.CleanupState != "clean" || pool.Len() != 1 {
					t.Fatalf("clean acknowledged lease not released: %+v available=%d", r, pool.Len())
				}
			} else {
				if r.CleanupState != "quarantined" || pool.Len() != 0 {
					t.Fatalf("%s released ambiguous UID: %+v available=%d", failure, r, pool.Len())
				}
				if _, err := os.Stat(filepath.Join(j.root+".uids", fmt.Sprintf("%d.json", uid))); err != nil {
					t.Fatal("quarantined lease metadata removed", err)
				}
			}
		})
	}
}

// Run explicitly as a nonroot user in the qualified cap-drop-all component
// runner. It must actually pass initialization beneath root-owned / and /tmp.
func TestNativeNonrootSupervisorLeaseAncestors(t *testing.T) {
	if os.Getenv("CS5_NONROOT_COMPONENT") != "1" {
		t.Skip("separate nonroot component invocation required")
	}
	if os.Geteuid() == 0 {
		t.Fatal("nonroot control invoked as root")
	}
	if _, err := uidpool.NewPersistentUIDPool(21000, 21002, filepath.Join(t.TempDir(), "leases")); err != nil {
		t.Fatal("service-owned lease under root-owned system ancestors rejected:", err)
	}
	p := noneCustodyProfile(t)
	s, err := NewSupervisor(p, func(context.Context, InvocationSpec) (Budget, error) { return p.Limits, nil }, func(InvocationSpec, Payload) (Launch, error) { return Launch{}, nil })
	if err != nil {
		t.Fatal("service-owned lease under root-owned system ancestors rejected:", err)
	}
	defer s.Close()
	if s.uidPool == nil {
		t.Fatal("nonroot none supervisor has no lease custody")
	}
}

// The helper exits without ReleaseChecked, so the kernel actually drops its
// flock. Resource cleanup really succeeds, but the terminal journal rename
// fails. A new pool must still refuse the old UID after that owner dies.
func TestNativeLeaseCrashBeforeTerminalAcknowledgement(t *testing.T) {
	if dir := os.Getenv("CS5_CUSTODY_CRASH_DIR"); dir != "" {
		p := Profile{WorkRoot: filepath.Join(dir, "work"), EnvironmentRoot: filepath.Join(dir, "environment"), CgroupRoot: filepath.Join(dir, "groups"), JournalRoot: filepath.Join(dir, "journal")}
		j, err := openJournal(p)
		if err != nil {
			t.Fatal(err)
		}
		pool, err := uidpool.NewPersistentUIDPool(21000, 21001, j.root+".uids")
		if err != nil {
			t.Fatal(err)
		}
		uid, err := pool.Acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		root := filepath.Join(p.WorkRoot, "request-crash")
		e := &Execution{profile: p, spec: InvocationSpec{ExecutionID: "crash", InvocationDigest: strings.Repeat("a", 64), Budget: validTestBudget()}, journal: j, uidPool: pool, hostUID: uid, WorkDir: root, done: make(chan struct{}), result: ExecutionResult{CleanupState: "clean"}}
		if err := e.saveJournal("queued"); err != nil {
			t.Fatal(err)
		}
		if err := pool.TrackResource(uid, root); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(root, 0700); err != nil {
			t.Fatal(err)
		}
		if err := e.cleanup(); err != nil {
			t.Fatal(err)
		}
		// Make only the terminal acknowledgement unwritable by atomic rename;
		// the containing private journal and lease resource paths remain stable.
		recordPath := filepath.Join(j.root, journalName("crash"))
		if err := os.Remove(recordPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(recordPath, 0700); err != nil {
			t.Fatal(err)
		}
		e.complete("succeeded", "")
		r, _ := e.Wait(context.Background())
		if r.Status != "unknown" || r.CleanupState != "quarantined" || pool.Len() != 0 {
			t.Fatal("terminal acknowledgement failure control:", r)
		}
		os.Exit(0)
	}
	dir := t.TempDir()
	for _, name := range []string{"work", "environment", "groups"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	child := exec.Command(os.Args[0], "-test.run=^TestNativeLeaseCrashBeforeTerminalAcknowledgement$")
	child.Env = append(os.Environ(), "CS5_CUSTODY_CRASH_DIR="+dir)
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("crash helper failed before custody assertion: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(dir, "work", "request-crash")); !os.IsNotExist(err) {
		t.Fatal("helper did not actually clean its root", err)
	}
	pool, err := uidpool.NewPersistentUIDPool(21000, 21001, filepath.Join(dir, "journal.uids"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if uid, err := pool.Acquire(ctx); err == nil {
		pool.ReleaseChecked(uid)
		t.Fatalf("UID %d reused after owner death and cleanup without terminal acknowledgement", uid)
	}
}

func TestNativeUIDCustodyCrashReconcile(t *testing.T) {
	if raw := os.Getenv("CS5_RECOVERY_PROFILE"); raw != "" {
		var p Profile
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatal(err)
		}
		j, err := openJournal(p)
		if err != nil {
			t.Fatal(err)
		}
		pool, err := uidpool.NewPersistentUIDPool(21000, 21002, j.root+".uids")
		if err != nil {
			t.Fatal(err)
		}
		uid, err := pool.Acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		mode := os.Getenv("CS5_RECOVERY_MODE")
		root := filepath.Join(p.WorkRoot, "request-crash")
		spec := testSpec(p)
		spec.InvocationDigest = InvocationDigest(spec, []byte(`{"code":"print(1)","preload":""}`))
		e := &Execution{profile: p, spec: spec, journal: j, uidPool: pool, hostUID: uid, WorkDir: root}
		if mode == "quarantined" {
			e.cgroup = filepath.Join(p.CgroupRoot, "request-crash")
			if err := pool.TrackResource(uid, e.cgroup); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(e.cgroup, 0700); err != nil {
				t.Fatal(err)
			}
		}
		if err := e.saveJournal("queued"); err != nil {
			t.Fatal(err)
		}
		if err := pool.TrackResource(uid, root); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(root, 0700); err != nil {
			t.Fatal(err)
		}
		if mode == "after-cleanup" || mode == "terminal-ack" || strings.HasPrefix(mode, "release-window-") {
			if err := e.cleanup(); err != nil {
				t.Fatal(err)
			}
		}
		if mode == "terminal-ack" || strings.HasPrefix(mode, "release-window-") {
			if err := e.saveJournal("terminal"); err != nil {
				t.Fatal(err)
			}
		}
		if strings.HasPrefix(mode, "release-window-") {
			if err := e.clearUIDCustody(); err != nil {
				t.Fatal(err)
			}
		}
		os.Exit(0)
	}
	for _, mode := range []string{"before-cleanup", "after-cleanup", "terminal-ack", "quarantined", "missing-marker", "corrupt-marker", "orphan-marker", "mismatched-uid", "missing-lease", "lease-digest", "lease-root", "lease-id", "release-window-missing-lease", "release-window-lease-digest"} {
		t.Run(mode, func(t *testing.T) {
			p := noneCustodyProfile(t)
			p.MaxConcurrent = 2
			p.JournalRoot = filepath.Join(t.TempDir(), "journal")
			raw, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			child := exec.Command(os.Args[0], "-test.run=^TestNativeUIDCustodyCrashReconcile$")
			child.Env = append(os.Environ(), "CS5_RECOVERY_PROFILE="+string(raw), "CS5_RECOVERY_MODE="+mode)
			if output, err := child.CombinedOutput(); err != nil {
				t.Fatalf("recovery helper: %v\n%s", err, output)
			}
			pending := filepath.Join(p.JournalRoot, strings.TrimSuffix(journalName("request-1"), ".json")+".uid-pending")
			if _, err := os.Stat(pending); err != nil && !strings.HasPrefix(mode, "release-window-") {
				t.Fatal("crashed execution did not retain pending acknowledgement", err)
			}
			record := filepath.Join(p.JournalRoot, journalName("request-1"))
			switch mode {
			case "missing-marker":
				err = os.Remove(pending)
			case "corrupt-marker":
				err = os.WriteFile(pending, []byte("torn"), 0600)
			case "orphan-marker":
				err = os.Remove(record)
			case "missing-lease", "release-window-missing-lease":
				err = os.Remove(filepath.Join(p.JournalRoot+".uids", "21000.json"))
			case "lease-digest", "lease-root", "lease-id", "release-window-lease-digest":
				leasePath := filepath.Join(p.JournalRoot+".uids", "21000.json")
				var data []byte
				data, err = os.ReadFile(leasePath)
				if err == nil {
					var lease map[string]any
					err = json.Unmarshal(data, &lease)
					if err == nil {
						if strings.HasSuffix(mode, "lease-digest") {
							lease["custody_digest"] = strings.Repeat("b", 64)
						} else {
							path := filepath.Join(p.JournalRoot, custodyName("different-id"))
							if mode == "lease-root" {
								path = filepath.Join(filepath.Dir(p.JournalRoot), custodyName("request-1"))
							}
							lease["custody"] = path
							lease["resources"] = []string{path}
						}
						data, err = json.Marshal(lease)
						if err == nil {
							err = os.WriteFile(leasePath, data, 0600)
						}
					}
				}
			case "mismatched-uid":
				var data []byte
				data, err = os.ReadFile(pending)
				if err == nil {
					err = os.WriteFile(pending, []byte(strings.Replace(string(data), "21000", "21001", 1)), 0600)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			s, err := NewSupervisor(p, func(context.Context, InvocationSpec) (Budget, error) { return p.Limits, nil }, func(InvocationSpec, Payload) (Launch, error) {
				t.Error("recovered identity dispatched again")
				return Launch{}, nil
			})
			if mode == "missing-marker" || mode == "corrupt-marker" || mode == "orphan-marker" || mode == "mismatched-uid" || mode == "missing-lease" || strings.HasPrefix(mode, "lease-") || strings.HasPrefix(mode, "release-window-") {
				if err == nil {
					s.Close()
					t.Fatal("torn UID custody admitted supervisor")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			body := `{"code":"print(1)","preload":""}`
			spec := testSpec(p)
			spec.InvocationDigest = InvocationDigest(spec, []byte(body))
			e, err := s.Start(context.Background(), spec, strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			result, _ := e.Wait(context.Background())
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			uid, err := s.uidPool.Acquire(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer s.uidPool.ReleaseChecked(uid)
			if mode == "quarantined" {
				if result.CleanupState != "quarantined" || uid != 21001 || s.admission.quarantined != 1 {
					t.Fatalf("quarantine lost UID/capacity custody: %+v UID=%d", result, uid)
				}
				if _, err := os.Stat(pending); err != nil {
					t.Fatal("quarantine removed pending acknowledgement", err)
				}
			} else {
				if result.Status != "unknown" || result.CleanupState != "clean" || uid != 21000 {
					t.Fatalf("clean recovery/replay: %+v UID=%d", result, uid)
				}
				if _, err := os.Stat(pending); !os.IsNotExist(err) {
					t.Fatal("acknowledged recovery retained marker", err)
				}
			}
		})
	}
}
