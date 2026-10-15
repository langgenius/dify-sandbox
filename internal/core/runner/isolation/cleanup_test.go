package isolation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCleanupQuarantine(t *testing.T) {
	p := testProfile(t)
	ticket, err := newAdmission(1, 1, p.MaxQueuedBytes).reserve(1)
	if err != nil {
		t.Fatal(err)
	}
	e := &Execution{spec: testSpec(p), profile: p, done: make(chan struct{}), release: ticket.release, result: ExecutionResult{CleanupState: "quarantined"}}
	e.complete("unknown", "cleanup_failed")
	if e.Snapshot().State != "quarantined" || ticket.released {
		t.Fatal("quarantined capacity released")
	}
}
func TestCancelQueuedAndLookupDigest(t *testing.T) {
	p := testProfile(t)
	s, err := NewSupervisor(p, func(context.Context, InvocationSpec) (Budget, error) { return p.Limits, nil }, func(InvocationSpec, Payload) (Launch, error) {
		t.Error("cancelled queue launched")
		return Launch{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	hold, err := s.admission.acquire(context.Background(), 1, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer hold()
	body := `{"code":"secret","preload":""}`
	spec := testSpec(p)
	spec.InvocationDigest = InvocationDigest(spec, []byte(body))
	e, err := s.Start(context.Background(), spec, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Lookup(spec.ExecutionID, strings.Repeat("f", 64)); !errors.Is(err, ErrInvocationConflict) {
		t.Fatalf("wrong digest: %v", err)
	}
	for i := 0; i < 2; i++ {
		r, err := e.Cancel(context.Background(), "cancelled")
		if err != nil || r.Status != "cancelled" {
			t.Fatalf("cancel: %+v %v", r, err)
		}
	}
}
func TestJournalRestartNoReplayAndPrivate(t *testing.T) {
	p := testProfile(t)
	calls := 0
	resolver := func(context.Context, InvocationSpec) (Budget, error) { return p.Limits, nil }
	launch := func(InvocationSpec, Payload) (Launch, error) { calls++; return Launch{}, nil }
	s, err := NewSupervisor(p, resolver, launch)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"code":"secret-code","preload":""}`
	spec := testSpec(p)
	spec.InvocationDigest = InvocationDigest(spec, []byte(body))
	e, err := s.Start(context.Background(), spec, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	e.Wait(context.Background())
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = NewSupervisor(p, resolver, launch)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e, err = s.Start(context.Background(), spec, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r, _ := e.Wait(context.Background())
	if r.Status != "unknown" || calls != 1 {
		t.Fatalf("restart replay: %+v calls=%d", r, calls)
	}
	entries, _ := os.ReadDir(p.WorkRoot + ".journal")
	for _, entry := range entries {
		data, _ := os.ReadFile(filepath.Join(p.WorkRoot+".journal", entry.Name()))
		if strings.Contains(string(data), "secret-code") {
			t.Fatal("journal contains code")
		}
	}
}

func TestJournalRejectsSymlinksOverlapAndSecondOwner(t *testing.T) {
	for _, kind := range []string{"symlink", "child", "world"} {
		t.Run(kind, func(t *testing.T) {
			p := testProfile(t)
			switch kind {
			case "symlink":
				p.JournalRoot = filepath.Join(t.TempDir(), "journal")
				os.Symlink(t.TempDir(), p.JournalRoot)
			case "child":
				p.JournalRoot = filepath.Join(p.WorkRoot, "journal")
			case "world":
				p.JournalRoot = t.TempDir()
				os.Chmod(p.JournalRoot, 0777)
			}
			if _, err := openJournal(p); err == nil {
				t.Fatal("unsafe journal accepted")
			}
		})
	}
	p := testProfile(t)
	j, err := openJournal(p)
	if err != nil {
		t.Fatal(err)
	}
	defer j.close()
	if other, err := openJournal(p); err == nil {
		other.close()
		t.Fatal("second supervisor admitted")
	}
}
func TestRestartQuarantinesFakeCgroupAndKeepsPeerCapacity(t *testing.T) {
	p := testProfile(t)
	p.MaxConcurrent = 2
	root := filepath.Join(p.WorkRoot, "request-orphan")
	os.Mkdir(root, 0700)
	cg := filepath.Join(p.CgroupRoot, "request-orphan")
	os.Mkdir(cg, 0700)
	j, err := openJournal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err = j.save(journalRecord{ID: "orphan", Digest: strings.Repeat("c", 64), Root: root, Cgroup: cg, State: "running", CleanupMillis: p.Limits.CleanupMillis}); err != nil {
		t.Fatal(err)
	}
	j.close()
	s, err := NewSupervisor(p, func(context.Context, InvocationSpec) (Budget, error) { return p.Limits, nil }, func(InvocationSpec, Payload) (Launch, error) { return Launch{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e, _ := s.Lookup("orphan", strings.Repeat("c", 64))
	if e.Snapshot().CleanupState != "quarantined" {
		t.Fatal("fake kernel cleanup accepted")
	}
	hold, err := s.admission.acquire(context.Background(), 1, time.Second)
	if err != nil {
		t.Fatal("healthy peer unavailable", err)
	}
	hold()
	if _, err = os.Stat(root); err != nil {
		t.Fatal("quarantined root erased")
	}
}
func TestJournalFailureRetainsCapacity(t *testing.T) {
	p := testProfile(t)
	j, err := openJournal(p)
	if err != nil {
		t.Fatal(err)
	}
	defer j.close()
	a := newAdmission(1, 1, p.MaxQueuedBytes)
	ticket, _ := a.reserve(1)
	e := &Execution{spec: testSpec(p), journal: j, profile: p, done: make(chan struct{}), release: ticket.release, result: ExecutionResult{CleanupState: "clean"}}
	os.Rename(j.root, j.root+"-lost")
	e.complete("succeeded", "")
	r, _ := e.Wait(context.Background())
	if r.Status != "unknown" || r.CleanupState != "quarantined" || ticket.released {
		t.Fatal("journal failure released capacity", r)
	}
}
func TestCancelledBeforeGateNeverBecomesConfigurationFailure(t *testing.T) {
	p := testProfile(t)
	e := &Execution{profile: p, spec: testSpec(p), done: make(chan struct{}), result: ExecutionResult{CleanupState: "clean"}}
	e.FailBeforeStart(context.Canceled)
	r, _ := e.Wait(context.Background())
	if r.Status != "cancelled" {
		t.Fatalf("cancelled request misclassified: %+v", r)
	}
}
func TestAllQuarantinedCapacityRefusesImmediately(t *testing.T) {
	a := newAdmission(1, 2, 100)
	ticket, _ := a.reserve(10)
	ticket.quarantine()
	started := time.Now()
	_, err := a.acquire(context.Background(), 10, time.Second)
	var f *Failure
	if !errors.As(err, &f) || f.Reason != "unavailable" || time.Since(started) > 100*time.Millisecond {
		t.Fatalf("quarantine queued unusable capacity: %v", err)
	}
}
func TestUnownedCgroupPreventsAdmissionAfterRestart(t *testing.T) {
	p := testProfile(t)
	os.Mkdir(filepath.Join(p.CgroupRoot, "request-lost"), 0700)
	s, err := NewSupervisor(p, func(context.Context, InvocationSpec) (Budget, error) { return p.Limits, nil }, func(InvocationSpec, Payload) (Launch, error) { return Launch{}, nil })
	if err == nil {
		s.Close()
		t.Fatal("unowned cgroup admitted new work")
	}
}
func TestCgroupSymlinkCleanupQuarantines(t *testing.T) {
	p := testProfile(t)
	cg := filepath.Join(p.CgroupRoot, "request-link")
	os.Symlink("/sys/fs/cgroup", cg)
	e := &Execution{profile: p, spec: testSpec(p), cgroup: cg}
	if err := e.cleanup(); err == nil {
		t.Fatal("symlink cleanup accepted")
	}
}
func TestReconciliationReapsOnlyOwnedProcessIdentity(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "sleep 10")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Release()
	start, err := processStartTime(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Process.Kill()
	if err = waitUnreaped(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	e := &Execution{ownedPID: cmd.Process.Pid, ownedStart: start}
	if err = e.reapOwned(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(fmt.Sprintf("/proc/%d", cmd.Process.Pid)); !os.IsNotExist(err) {
		t.Fatal("owned zombie not reaped")
	}
	e = &Execution{ownedPID: os.Getpid(), ownedStart: "not-the-same-process"}
	if err = e.reapOwned(); err != nil {
		t.Fatal("reused PID incorrectly owned", err)
	}
}
