package cases

import (
	"context"
	"fmt"
	"github.com/langgenius/dify-sandbox/internal/core/runner/isolation"
	"github.com/langgenius/dify-sandbox/internal/core/runner/network"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func procFixture(t *testing.T) (procReader, isolation.Observation, ProcessIdentity) {
	t.Helper()
	root := t.TempDir()
	reader := procReader{root: filepath.Join(root, "proc")}
	cg := filepath.Join(root, "request-a")
	if err := os.MkdirAll(cg, 0700); err != nil {
		t.Fatal(err)
	}
	put := func(path, value string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []int{100, 101, 102} {
		base := reader.path(id, "")
		for _, dir := range []string{"ns", "fd", "root/work"} {
			if err := os.MkdirAll(filepath.Join(base, dir), 0700); err != nil {
				t.Fatal(err)
			}
		}
		put(filepath.Join(base, "stat"), fmt.Sprintf("%d (native) S %s1234\n", id, strings.Repeat("0 ", 18)))
		tgid, nspid, exe := 100, id-99, "/usr/bin/python3.12"
		if id == 101 {
			tgid = 101
			exe = "/usr/bin/network-gateway"
		}
		put(filepath.Join(base, "status"), fmt.Sprintf("Tgid:\t%d\nNSpid:\t%d %d\nUid:\t20000 20000 20000 20000\nGid:\t999 999 999 999\nCapEff:\t00000000\nCapPrm:\t00000000\nCapAmb:\t00000000\n", tgid, id, nspid))
		put(filepath.Join(base, "cgroup"), "0::/owned/request-a\n")
		put(filepath.Join(base, "fd/3"), "fixed synthetic fd")
		put(filepath.Join(base, "root/work/private"), "fixed synthetic file")
		for link, target := range map[string]string{"ns/user": "user:[6]", "ns/pid": "pid:[2]", "exe": exe} {
			if err := os.Symlink(target, filepath.Join(base, link)); err != nil {
				t.Fatal(err)
			}
		}
	}
	put(filepath.Join(cg, "cgroup.procs"), "100\n101\n")
	put(filepath.Join(cg, "cgroup.threads"), "100\n101\n102\n")
	put(filepath.Join(cg, "pids.current"), "3\n")
	o := completedCustody().Observation
	o.Cgroup = cg
	gated, err := reader.gatedPayload(o)
	if err != nil {
		t.Fatal(err)
	}
	if gated.PID != 100 {
		t.Fatal("helper selected as payload", gated)
	}
	return reader, o, gated
}
func TestProcSamplerCannotHideLivePayloadBehindHelper(t *testing.T) {
	for _, bad := range []string{"none", "wrong-payload-uid", "missing-live-status", "privileged-thread", "missing-executable"} {
		t.Run(bad, func(t *testing.T) {
			p, o, g := procFixture(t)
			switch bad {
			case "wrong-payload-uid":
				path := p.path(100, "status")
				raw, _ := os.ReadFile(path)
				os.WriteFile(path, []byte(strings.ReplaceAll(string(raw), "20000", "999")), 0600)
			case "missing-live-status":
				if err := os.Remove(p.path(100, "status")); err != nil {
					t.Fatal(err)
				}
			case "privileged-thread":
				path := p.path(102, "status")
				raw, _ := os.ReadFile(path)
				os.WriteFile(path, []byte(strings.Replace(string(raw), "CapPrm:\t00000000", "CapPrm:\t00000001", 1)), 0600)
			case "missing-executable":
				if err := os.Remove(p.path(100, "exe")); err != nil {
					t.Fatal(err)
				}
			}
			c, _, _, _, err := p.capture(o, g, "python3", "scoped")
			if bad == "none" {
				if err != nil || c == nil || len(c.Threads) != 3 {
					t.Fatalf("valid all-thread snapshot failed: %+v %v", c, err)
				}
			} else if err == nil {
				t.Fatal("live unsafe/unavailable payload silently omitted")
			}
		})
	}
}
func TestMissingTaskRequiresIndependentExitAndMembershipProof(t *testing.T) {
	p, o, _ := procFixture(t)
	if err := os.RemoveAll(p.path(102, "")); err != nil {
		t.Fatal(err)
	}
	if p.gone(102, "1234", o.Cgroup) {
		t.Fatal("proc absence alone accepted while task remains in cgroup membership")
	}
	if err := os.WriteFile(filepath.Join(o.Cgroup, "cgroup.threads"), []byte("100\n101\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if !p.gone(102, "1234", o.Cgroup) {
		t.Fatal("proven normal exit rejected")
	}
}

func TestUnreapedPayloadRequiresTerminalStateAndAbsentMembership(t *testing.T) {
	for _, scenario := range []string{"zombie-left-group", "live-left-group", "zombie-still-listed", "zombie-wrong-start"} {
		t.Run(scenario, func(t *testing.T) {
			p, o, g := procFixture(t)
			statPath := p.path(g.PID, "stat")
			raw, err := os.ReadFile(statPath)
			if err != nil {
				t.Fatal(err)
			}
			if scenario != "live-left-group" {
				raw = []byte(strings.Replace(string(raw), ") S ", ") Z ", 1))
			}
			if scenario == "zombie-wrong-start" {
				raw = []byte(strings.Replace(string(raw), "1234", "5678", 1))
			}
			if err = os.WriteFile(statPath, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if err = os.Remove(p.path(g.PID, "exe")); err != nil {
				t.Fatal(err)
			}
			if scenario != "zombie-still-listed" && scenario != "zombie-wrong-start" {
				if err = os.WriteFile(filepath.Join(o.Cgroup, "cgroup.threads"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			frame, _, _, _, err := p.capture(o, g, "python3", "none")
			if scenario == "zombie-left-group" {
				if err != nil || frame != nil {
					t.Fatalf("same-start terminal nonmember produced failure/live frame: %+v %v", frame, err)
				}
			} else if err == nil {
				t.Fatal("live/inconsistent/reused zombie identity accepted as normal exit")
			}
		})
	}
}

func TestSamplerDistinguishesRemovedCgroupAfterExitFromLiveEvidenceLoss(t *testing.T) {
	for _, exited := range []bool{false, true} {
		t.Run(fmt.Sprint(exited), func(t *testing.T) {
			p, o, g := procFixture(t)
			if err := os.RemoveAll(o.Cgroup); err != nil {
				t.Fatal(err)
			}
			if exited {
				if err := os.RemoveAll(p.path(g.PID, "")); err != nil {
					t.Fatal(err)
				}
			}
			c, _, _, _, err := p.capture(o, g, "python3", "none")
			if exited {
				if err != nil || c != nil {
					t.Fatalf("proven exit/removal became unsafe or positive live evidence: %+v %v", c, err)
				}
			} else if err == nil {
				t.Fatal("missing cgroup accepted while payload is still live")
			}
		})
	}
}

func TestResourceRecorderRetainsImmutableGateControls(t *testing.T) {
	p, o, _ := procFixture(t)
	o.Phase = "gated"
	r := NewResourceRecorder(t.TempDir(), 4)
	r.proc = p
	if err := r.Expect(isolation.InvocationSpec{ExecutionID: o.ExecutionID, Language: "python3", Network: network.NetworkScope{Mode: "none"}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Observe(o); err != nil {
		t.Fatal(err)
	}
	defer func() {
		o.Phase = "completed"
		if err := r.Observe(o); err != nil {
			t.Error(err)
		}
	}()
	o.Controls["uid_map"] = "mutated completion control"
	o.Namespaces["user"] = "mutated completion namespace"
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entries[o.ExecutionID].gate.Controls["uid_map"] == o.Controls["uid_map"] || r.entries[o.ExecutionID].gate.Namespaces["user"] == o.Namespaces["user"] {
		t.Fatal("poller shares mutable supervisor control maps")
	}
}

func TestRecorderRequiresReadyAndRetainsLiveInspectionFailure(t *testing.T) {
	p, o, _ := procFixture(t)
	o.Phase = "gated"
	dir := t.TempDir()
	r := NewResourceRecorder(dir, 4)
	r.proc = p
	if err := r.Expect(isolation.InvocationSpec{ExecutionID: o.ExecutionID, Language: "python3", Network: network.NetworkScope{Mode: "none"}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Observe(o); err != nil {
		t.Fatal(err)
	}
	defer func() {
		o.Phase = "completed"
		if err := r.Observe(o); err != nil {
			t.Error(err)
		}
		<-r.entries[o.ExecutionID].done
	}()
	if _, err := readNativeResource(dir, o.ExecutionID, "python3", "none"); err == nil {
		t.Fatal("setup became post-ready evidence")
	}
	if err := r.Observe(isolation.Observation{ExecutionID: o.ExecutionID, Phase: "stdout", StdoutChunk: "CS1_READY\n"}); err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if _, err := waitNative(ctx, dir, o.ExecutionID, "python3", "none", 0, false); err != nil {
		t.Fatal(err)
	}
	// Removal of a still-listed thread's status must make the error sticky,
	// even though the innocent helper and earlier valid samples remain visible.
	status := p.path(100, "status")
	raw, err := os.ReadFile(status)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(status); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(time.Second)
	for {
		r.mu.Lock()
		failed := r.entries[o.ExecutionID].sample.Error != ""
		r.mu.Unlock()
		if failed {
			break
		}
		select {
		case <-deadline:
			t.Fatal("live inspection failure was ignored")
		case <-time.After(time.Millisecond):
		}
	}
	if err = os.WriteFile(status, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readNativeResource(dir, o.ExecutionID, "python3", "none"); err == nil {
		t.Fatal("earlier valid sample concealed failed live custody")
	}
}

func TestRecorderPreservesPriorEvidenceAcrossUnreapedExit(t *testing.T) {
	p, o, _ := procFixture(t)
	o.Phase = "gated"
	dir := t.TempDir()
	r := NewResourceRecorder(dir, 4)
	r.proc = p
	if err := r.Expect(isolation.InvocationSpec{ExecutionID: o.ExecutionID, Language: "python3", Network: network.NetworkScope{Mode: "none"}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Observe(o); err != nil {
		t.Fatal(err)
	}
	defer func() {
		o.Phase = "completed"
		if err := r.Observe(o); err != nil {
			t.Error(err)
		}
		<-r.entries[o.ExecutionID].done
		if _, err := readNativeResource(dir, o.ExecutionID, "python3", "none"); err != nil {
			t.Error("clean completion lost the prior verified native frame", err)
		}
	}()
	if err := r.Observe(isolation.Observation{ExecutionID: o.ExecutionID, Phase: "stdout", StdoutChunk: "CS1_READY\n"}); err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if _, err := waitNative(ctx, dir, o.ExecutionID, "python3", "none", 0, false); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	before := r.entries[o.ExecutionID].sample.NativeSamples
	statPath := p.path(100, "stat")
	raw, err := os.ReadFile(statPath)
	if err == nil {
		err = os.WriteFile(statPath, []byte(strings.Replace(string(raw), ") S ", ") Z ", 1)), 0600)
	}
	if err == nil {
		err = os.Remove(p.path(100, "exe"))
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(o.Cgroup, "cgroup.threads"), nil, 0600)
	}
	r.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	r.mu.Lock()
	sample := r.entries[o.ExecutionID].sample
	r.mu.Unlock()
	if err := RequireNativeResourceSample(sample); err != nil {
		t.Fatal("proven unreaped exit invalidated prior good evidence", err)
	}
	if sample.NativeSamples != before {
		t.Fatal("unreaped exit supplied fresh positive native evidence")
	}
}
