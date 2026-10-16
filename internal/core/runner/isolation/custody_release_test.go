package isolation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner/uidpool"
	sg "github.com/seccomp/libseccomp-golang"
	"golang.org/x/sys/unix"
)

func TestNativeFinalReleaseFailureRecoversAfterOwnerDeath(t *testing.T) {
	if raw := os.Getenv("CS5_RELEASE_PROFILE"); raw != "" {
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
		spec := testSpec(p)
		spec.InvocationDigest = InvocationDigest(spec, []byte(`{"code":"print(1)","preload":""}`))
		root := filepath.Join(p.WorkRoot, "request-final-release")
		cg := filepath.Join(p.CgroupRoot, "request-final-release")
		e := &Execution{profile: p, spec: spec, WorkDir: root, cgroup: cg, journal: j, uidPool: pool, hostUID: uid, done: make(chan struct{}), result: ExecutionResult{CleanupState: "clean"}}
		if err := e.saveJournal("queued"); err != nil {
			t.Fatal(err)
		}
		if err := pool.TrackResource(uid, root); err != nil {
			t.Fatal(err)
		}
		if err := pool.TrackResource(uid, cg); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(root, 0700); err != nil {
			t.Fatal(err)
		}
		if err := e.cleanup(); err != nil {
			t.Fatal(err)
		}
		// A late resource appears after the first physical cleanup. The final
		// independent release check must catch it and preserve recoverable custody.
		if err := os.Mkdir(cg, 0700); err != nil {
			t.Fatal(err)
		}
		denyFsync := func() {
			t.Helper()
			filter, err := sg.NewFilter(sg.ActAllow)
			if err != nil {
				t.Fatal(err)
			}
			defer filter.Release()
			call, err := sg.GetSyscallFromName("fsync")
			if err != nil {
				t.Fatal(err)
			}
			if err := filter.AddRule(call, sg.ActErrno.SetReturnCode(int16(unix.EIO))); err != nil {
				t.Fatal(err)
			}
			if err := filter.Load(); err != nil {
				t.Fatal(err)
			}
		}
		fault := os.Getenv("CS5_RELEASE_FAULT")
		if fault != "retained-resource" {
			if err := e.saveJournal("terminal"); err != nil {
				t.Fatal(err)
			}
		}
		if fault == "unlink-sync" {
			// Real unlink succeeds; the kernel returns EIO for the following
			// fsync. The filter is installed only in this exiting test helper.
			denyFsync()
			if err := e.clearUIDCustody(); err == nil {
				t.Fatal("directory fsync fault did not reach custody removal")
			}
			if _, err := os.Stat(filepath.Join(j.root, custodyName(spec.ExecutionID))); !os.IsNotExist(err) {
				t.Fatal("fault did not occur after actual unlink", err)
			}
		} else if fault == "retained-resource" {
			e.complete("succeeded", "")
			r, _ := e.Wait(context.Background())
			if r.Status != "unknown" || r.CleanupState != "quarantined" || pool.Len() != 1 {
				t.Fatalf("final release fault control: %+v available=%d", r, pool.Len())
			}
		} else {
			if err := e.clearUIDCustody(); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(cg); err != nil {
				t.Fatal(err)
			}
			leaseRoot := j.root + ".uids"
			leasePath := filepath.Join(leaseRoot, "21000.json")
			var fileLimit unix.Rlimit
			switch fault {
			case "lease-create":
				if err := os.Chmod(leaseRoot, 0500); err != nil {
					t.Fatal(err)
				}
			case "lease-rename":
				if err := os.Rename(leasePath, leasePath+".held"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(leasePath, 0700); err != nil {
					t.Fatal(err)
				}
			case "lease-sync":
				denyFsync()
			case "lease-write":
				signal.Ignore(unix.SIGXFSZ)
				if err := unix.Getrlimit(unix.RLIMIT_FSIZE, &fileLimit); err != nil {
					t.Fatal(err)
				}
				if err := unix.Setrlimit(unix.RLIMIT_FSIZE, &unix.Rlimit{Cur: 0, Max: fileLimit.Max}); err != nil {
					t.Fatal(err)
				}
			default:
				t.Fatal("invalid release fault")
			}
			if err := pool.ReleaseCustody(uid); err == nil {
				t.Fatal("final lease metadata fault did not fail release")
			}
			if fault == "lease-write" {
				if err := unix.Setrlimit(unix.RLIMIT_FSIZE, &fileLimit); err != nil {
					t.Fatal(err)
				}
			}
			if fault == "lease-create" {
				if err := os.Chmod(leaseRoot, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if fault == "lease-rename" {
				if err := os.Remove(leasePath); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(leasePath+".held", leasePath); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Mkdir(cg, 0700); err != nil {
				t.Fatal(err)
			}
		}
		os.Exit(0)
	}
	for _, fault := range []string{"retained-resource", "unlink-sync", "lease-create", "lease-write", "lease-rename", "lease-sync"} {
		t.Run(fault, func(t *testing.T) {
			p := noneCustodyProfile(t)
			p.MaxConcurrent = 2
			p.JournalRoot = filepath.Join(t.TempDir(), "journal")
			raw, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			child := exec.Command(os.Args[0], "-test.run=^TestNativeFinalReleaseFailureRecoversAfterOwnerDeath$")
			child.Env = append(os.Environ(), "CS5_RELEASE_PROFILE="+string(raw), "CS5_RELEASE_FAULT="+fault)
			if output, err := child.CombinedOutput(); err != nil {
				t.Fatalf("release fault helper: %v\n%s", err, output)
			}
			open := func() *Supervisor {
				t.Helper()
				s, err := NewSupervisor(p, func(context.Context, InvocationSpec) (Budget, error) { return p.Limits, nil }, func(InvocationSpec, Payload) (Launch, error) {
					t.Error("recovered identity redispatched")
					return Launch{}, nil
				})
				if err != nil {
					t.Fatal("release failure prevented healthy-peer recovery:", err)
				}
				return s
			}
			s := open()
			defer s.Close()
			spec := testSpec(p)
			body := `{"code":"print(1)","preload":""}`
			spec.InvocationDigest = InvocationDigest(spec, []byte(body))
			e, err := s.Start(context.Background(), spec, strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			r, _ := e.Wait(context.Background())
			if r.CleanupState != "quarantined" || s.admission.quarantined != 1 {
				t.Fatalf("release failure lost old slot: %+v", r)
			}
			if _, err := os.Stat(filepath.Join(p.JournalRoot, custodyName(spec.ExecutionID))); err != nil {
				t.Fatal("release failure lost recoverable pending marker:", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			uid, err := s.uidPool.Acquire(ctx)
			if err != nil || uid != 21001 {
				t.Fatalf("old UID reused or healthy peer blocked: %d %v", uid, err)
			}
			if err := s.uidPool.ReleaseChecked(uid); err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(p.CgroupRoot, "request-final-release")); err != nil {
				t.Fatal(err)
			}
			recovered := open()
			defer recovered.Close()
			uid, err = recovered.uidPool.Acquire(ctx)
			if err != nil || uid != 21000 {
				t.Fatalf("proved cleanup did not reclaim old UID: %d %v", uid, err)
			}
			if err := recovered.uidPool.ReleaseChecked(uid); err != nil {
				t.Fatal(err)
			}
			runtime.KeepAlive(s)
		})
	}
}

func TestNativeSupervisorCloseReleasesQuarantineHandles(t *testing.T) {
	oldGC := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(oldGC)
	countFDs := func() int {
		t.Helper()
		entries, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			t.Fatal(err)
		}
		return len(entries)
	}
	p := noneCustodyProfile(t)
	p.MaxConcurrent = 2
	before := countFDs()
	s, err := NewSupervisor(p, func(context.Context, InvocationSpec) (Budget, error) { return p.Limits, nil }, func(InvocationSpec, Payload) (Launch, error) { return Launch{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	uid, err := s.uidPool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	spec := testSpec(p)
	spec.InvocationDigest = strings.Repeat("a", 64)
	e := &Execution{profile: p, spec: spec, journal: s.journal, uidPool: s.uidPool, hostUID: uid, done: make(chan struct{}), result: ExecutionResult{CleanupState: "quarantined"}}
	if err := e.saveJournal("queued"); err != nil {
		t.Fatal(err)
	}
	e.complete("unknown", "cleanup_failed")
	if _, _, err := s.registry.register(spec.ExecutionID, spec.InvocationDigest, e); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if after := countFDs(); after != before {
		t.Fatalf("Close retained lease descriptors with old supervisor reachable and GC disabled: before=%d after=%d", before, after)
	}
	if _, err := os.Stat(filepath.Join(s.journal.root, custodyName(spec.ExecutionID))); err != nil {
		t.Fatal("Close erased durable quarantine:", err)
	}
	reopened, err := NewSupervisor(p, func(context.Context, InvocationSpec) (Budget, error) { return p.Limits, nil }, func(InvocationSpec, Payload) (Launch, error) { return Launch{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	uid, err = reopened.uidPool.Acquire(ctx)
	if err != nil || uid != 21000 {
		t.Fatalf("same-process recovery still depends on old handle finalizer: %d %v", uid, err)
	}
	if err := reopened.uidPool.ReleaseChecked(uid); err != nil {
		t.Fatal(err)
	}
	runtime.KeepAlive(s)
	runtime.KeepAlive(e)
}

func TestNativeCustodyBirthWindowFailsBeforeAdmission(t *testing.T) {
	if raw := os.Getenv("CS5_BIRTH_PROFILE"); raw != "" {
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
		spec := testSpec(p)
		spec.InvocationDigest = strings.Repeat("a", 64)
		if err := j.save(journalRecord{ID: spec.ExecutionID, Digest: spec.InvocationDigest, State: "queued", CleanupMillis: p.Limits.CleanupMillis}); err != nil {
			t.Fatal(err)
		}
		if err := pool.TrackCustody(uid, filepath.Join(j.root, custodyName(spec.ExecutionID)), spec.InvocationDigest); err != nil {
			t.Fatal(err)
		}
		// The owner dies with a durable binding, before marker creation and
		// before the queued journal acquires HostUID. No resource can identify it.
		os.Exit(0)
	}
	p := noneCustodyProfile(t)
	p.JournalRoot = filepath.Join(t.TempDir(), "journal")
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestNativeCustodyBirthWindowFailsBeforeAdmission$")
	child.Env = append(os.Environ(), "CS5_BIRTH_PROFILE="+string(raw))
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("birth helper: %v\n%s", err, output)
	}
	s, err := NewSupervisor(p, func(context.Context, InvocationSpec) (Budget, error) { return p.Limits, nil }, func(InvocationSpec, Payload) (Launch, error) {
		t.Fatal("unattributed lease admitted")
		return Launch{}, nil
	})
	if err == nil {
		s.Close()
		t.Fatal("unattributed durable custody admitted after owner death")
	}
	pool, err := uidpool.NewPersistentUIDPool(21000, 21002, p.JournalRoot+".uids")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	uid, err := pool.Acquire(ctx)
	if err != nil || uid != 21001 {
		t.Fatalf("unattributed old UID reused or peer unavailable: %d %v", uid, err)
	}
	if err := pool.ReleaseChecked(uid); err != nil {
		t.Fatal(err)
	}
}

func TestNativeReleasedHistorySurvivesUIDReuse(t *testing.T) {
	p := noneCustodyProfile(t)
	p.WorkerPath = "/nonexistent-request-worker"
	resolver := func(context.Context, InvocationSpec) (Budget, error) { return p.Limits, nil }
	calls := 0
	factory := func(InvocationSpec, Payload) (Launch, error) {
		calls++
		return Launch{Executable: "/bin/false", BootstrapPath: "/work/bootstrap"}, nil
	}
	s, err := NewSupervisor(p, resolver, factory)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"code":"print(1)","preload":""}`
	var specs []InvocationSpec
	for i := 0; i < 3; i++ {
		spec := testSpec(p)
		spec.ExecutionID = fmt.Sprintf("reuse-%d", i)
		spec.InvocationDigest = InvocationDigest(spec, []byte(body))
		specs = append(specs, spec)
		e, err := s.Start(context.Background(), spec, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r, err := e.Wait(context.Background())
		if err != nil || r.CleanupState != "clean" || e.hostUID != 21000+i%2 {
			t.Fatalf("finite lease reuse %d: UID=%d result=%+v err=%v", i, e.hostUID, r, err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewSupervisor(p, resolver, factory)
	if err != nil {
		t.Fatal("released history rejected newer lease identity:", err)
	}
	defer reopened.Close()
	for _, spec := range specs {
		e, err := reopened.Start(context.Background(), spec, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r, err := e.Wait(context.Background())
		if err != nil || r.Status != "unknown" || r.CleanupState != "clean" || calls != 3 {
			t.Fatalf("historical identity redispatched: %+v err=%v calls=%d", r, err, calls)
		}
	}
	runtime.KeepAlive(s)
}

func TestNativeRepeatedReleaseWindowRecoveryKeepsBoundedCustody(t *testing.T) {
	if raw := os.Getenv("CS5_REPEAT_PROFILE"); raw != "" {
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
		spec := testSpec(p)
		spec.InvocationDigest = InvocationDigest(spec, []byte(`{"code":"print(1)","preload":""}`))
		root := filepath.Join(p.WorkRoot, "request-repeated")
		cg := filepath.Join(p.CgroupRoot, "request-repeated")
		e := &Execution{profile: p, spec: spec, journal: j, uidPool: pool, hostUID: 21000, WorkDir: root, cgroup: cg}
		if os.Getenv("CS5_REPEAT_FIRST") == "true" {
			uid, err := pool.Acquire(context.Background())
			if err != nil || uid != e.hostUID {
				t.Fatalf("first lease %d %v", uid, err)
			}
			if err := pool.TrackResource(uid, root); err != nil {
				t.Fatal(err)
			}
			if err := pool.TrackResource(uid, cg); err != nil {
				t.Fatal(err)
			}
			if err := e.saveJournal("queued"); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
		} else {
			data, err := os.ReadFile(filepath.Join(j.root, journalName(spec.ExecutionID)))
			if err != nil {
				t.Fatal(err)
			}
			var record journalRecord
			if err := decodeStrict(data, &record); err != nil {
				t.Fatal(err)
			}
			if record.State != "terminal" || !record.UIDReleasePending || record.HostUID != e.hostUID || record.Digest != spec.InvocationDigest {
				t.Fatal("missing durable release window")
			}
			owned, err := pool.RecoverCustody(e.hostUID, filepath.Join(j.root, custodyName(spec.ExecutionID)), spec.InvocationDigest, true)
			if err != nil || !owned {
				t.Fatalf("exact UID recovery: owned=%v err=%v", owned, err)
			}
			e.uidReleasePending = true
			if err := e.recordUIDCustody(); err != nil {
				t.Fatal(err)
			}
		}
		if err := e.cleanup(); err != nil {
			t.Fatal(err)
		}
		if err := e.saveJournal("terminal"); err != nil {
			t.Fatal(err)
		}
		if err := e.clearUIDCustody(); err != nil {
			t.Fatal(err)
		}
		// Each process really creates/syncs the marker, acknowledges cleanup,
		// unlinks/syncs it, then dies without releasing the retained lease.
		os.Exit(0)
	}
	p := noneCustodyProfile(t)
	longParent := t.TempDir()
	for i := 0; i < 10; i++ {
		longParent = filepath.Join(longParent, strings.Repeat("p", 120))
	}
	if err := os.MkdirAll(longParent, 0700); err != nil {
		t.Fatal(err)
	}
	p.JournalRoot = filepath.Join(longParent, "journal")
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	pending := filepath.Join(p.JournalRoot, custodyName("request-1"))
	cycles := 16384/len(pending) + 4
	initialSize := 0
	for i := 0; i < cycles; i++ {
		child := exec.Command(os.Args[0], "-test.run=^TestNativeRepeatedReleaseWindowRecoveryKeepsBoundedCustody$")
		child.Env = append(os.Environ(), "GORACE=halt_on_error=1 atexit_sleep_ms=0", "CS5_REPEAT_PROFILE="+string(raw), fmt.Sprintf("CS5_REPEAT_FIRST=%t", i == 0))
		if output, err := child.CombinedOutput(); err != nil {
			t.Fatalf("owner death/recovery cycle %d: %v\n%s", i, err, output)
		}
		data, err := os.ReadFile(filepath.Join(p.JournalRoot+".uids", "21000.json"))
		if err != nil {
			t.Fatal(err)
		}
		var lease struct {
			Resources []string `json:"resources"`
		}
		if err := json.Unmarshal(data, &lease); err != nil {
			t.Fatal(err)
		}
		counts := make(map[string]int)
		for _, path := range lease.Resources {
			counts[path]++
		}
		if counts[pending] != 1 || counts[filepath.Join(p.WorkRoot, "request-repeated")] != 1 || counts[filepath.Join(p.CgroupRoot, "request-repeated")] != 1 || len(lease.Resources) != 3 {
			t.Errorf("cycle %d lost idempotent custody or other resources: marker=%d resources=%d", i, counts[pending], len(lease.Resources))
		}
		if i == 0 {
			initialSize = len(data)
		}
		if len(data) != initialSize || len(data) > 16384 {
			t.Errorf("cycle %d metadata grew: first=%d current=%d", i, initialSize, len(data))
		}
		if _, err := os.Stat(pending); !os.IsNotExist(err) {
			t.Fatal("helper did not finish actual unlink", err)
		}
	}
	s, err := NewSupervisor(p, func(context.Context, InvocationSpec) (Budget, error) { return p.Limits, nil }, func(InvocationSpec, Payload) (Launch, error) {
		t.Error("recovered identity redispatched")
		return Launch{}, nil
	})
	if err != nil {
		t.Fatal("final successful recovery:", err)
	}
	defer s.Close()
	body := `{"code":"print(1)","preload":""}`
	spec := testSpec(p)
	spec.InvocationDigest = InvocationDigest(spec, []byte(body))
	e, err := s.Start(context.Background(), spec, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.Wait(context.Background())
	if err != nil || r.Status != "unknown" || r.CleanupState != "clean" {
		t.Fatalf("recovery result %+v %v", r, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	uid, err := s.uidPool.Acquire(ctx)
	if err != nil || uid != 21000 {
		t.Fatalf("same UID not reusable after successful cleanup: %d %v", uid, err)
	}
	if err := s.uidPool.ReleaseChecked(uid); err != nil {
		t.Fatal(err)
	}
}

func TestNativeCustodyBindingRejectsMismatchWithoutMutation(t *testing.T) {
	for _, mismatch := range []string{"path", "digest"} {
		t.Run(mismatch, func(t *testing.T) {
			p := noneCustodyProfile(t)
			j, err := openJournal(p)
			if err != nil {
				t.Fatal(err)
			}
			defer j.close()
			pool, err := uidpool.NewPersistentUIDPool(21000, 21002, j.root+".uids")
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			uid, err := pool.Acquire(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(j.root, custodyName("request-1"))
			digest := strings.Repeat("a", 64)
			if err := pool.TrackCustody(uid, path, digest); err != nil {
				t.Fatal(err)
			}
			leasePath := filepath.Join(j.root+".uids", "21000.json")
			before, err := os.ReadFile(leasePath)
			if err != nil {
				t.Fatal(err)
			}
			if mismatch == "path" {
				path = filepath.Join(j.root, custodyName("wrong-id"))
			} else {
				digest = strings.Repeat("b", 64)
			}
			if err := pool.TrackCustody(uid, path, digest); err == nil {
				t.Fatal("mismatched binding accepted")
			}
			after, err := os.ReadFile(leasePath)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatal("rejected custody binding changed durable metadata")
			}
		})
	}
}
