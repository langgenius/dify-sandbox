package isolation

import (
	"context"
	"errors"
	"fmt"
	"github.com/langgenius/dify-sandbox/internal/core/runner/network"
	"github.com/langgenius/dify-sandbox/internal/core/runner/uidpool"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScopedCapabilityRefusedBeforeLaunch(t *testing.T) {
	p := testProfile(t)
	sup, err := NewSupervisor(p, func(context.Context, InvocationSpec) (Budget, error) { return p.Limits, nil }, func(InvocationSpec, Payload) (Launch, error) {
		t.Error("unsupported scope reached launch")
		return Launch{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sup.Close()
	spec := testSpec(p)
	spec.Network = network.NetworkScope{Mode: "scoped", Destinations: []network.Destination{{Protocol: "tcp", Host: "canary.example", Port: 443, Addresses: []string{"192.0.2.10"}}}, InputManifestDigest: strings.Repeat("a", 64), MaxEgressBytes: 1024, ExpiresAt: time.Now().Add(time.Minute).Unix()}
	spec.Network.ScopeDigest = network.ScopeDigest(spec.Network)
	payload := `{"code":"must not launch","preload":""}`
	spec.InvocationDigest = InvocationDigest(spec, []byte(payload))
	e, err := sup.Start(context.Background(), spec, strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	result, err := e.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "configuration_required" || result.FailureReason != "network_capability" || e.WorkDir != "" || e.cmd != nil {
		t.Fatalf("unsupported scope not refused before launch: %+v", result)
	}
}

func TestQualifiedHTTPProfileBoundsEgressAndUIDRange(t *testing.T) {
	p := testProfile(t)
	p.HTTPProxyQualified = true
	p.HostUIDMin = 20000
	p.HostUIDMax = 20004
	p.MaxNetworkEgressBytes = 4096
	s := network.NetworkScope{Mode: "scoped", Destinations: []network.Destination{{Protocol: "http", Host: "canary.example", Port: 80, Addresses: []string{"192.0.2.10"}}}, InputManifestDigest: strings.Repeat("a", 64), MaxEgressBytes: 1024, ExpiresAt: time.Now().Add(time.Minute).Unix()}
	s.ScopeDigest = network.ScopeDigest(s)
	if !p.Supports(s) {
		t.Fatal("qualified bounded HTTP profile unsupported")
	}
	s.MaxEgressBytes = 4097
	s.ScopeDigest = network.ScopeDigest(s)
	if p.Supports(s) {
		t.Fatal("scope expanded profile egress budget")
	}
	for _, bounds := range [][2]int{{0, 10}, {10000, 11000}, {65536, 65538}, {20000, 20000}, {20000, 30000}} {
		p.HostUIDMin, p.HostUIDMax = bounds[0], bounds[1]
		if p.Validate() == nil {
			t.Fatal("unsafe UID range accepted")
		}
	}
}

func TestServerReservationDeniesRecomputedClientHashesBeforeOwnership(t *testing.T) {
	p := testProfile(t)
	original := testSpec(p)
	payload := `{"code":"approved opaque script","preload":""}`
	original.Network = network.NetworkScope{Mode: "scoped", Destinations: []network.Destination{{Protocol: "http", Host: "canary.example", Port: 80, Addresses: []string{"192.0.2.10"}}}, InputManifestDigest: strings.Repeat("a", 64), MaxEgressBytes: 1024, ExpiresAt: time.Now().Add(time.Minute).Unix()}
	original.Network.ScopeDigest = network.ScopeDigest(original.Network)
	original.InvocationDigest = InvocationDigest(original, []byte(payload))
	sup, err := NewSupervisor(p, func(_ context.Context, s InvocationSpec) (Budget, error) {
		if s.InvocationDigest != original.InvocationDigest {
			return Budget{}, fmt.Errorf("server reservation mismatch")
		}
		return p.Limits, nil
	}, func(InvocationSpec, Payload) (Launch, error) {
		t.Error("unapproved payload reached launch")
		return Launch{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sup.Close()
	changed := original
	changed.Network.InputManifestDigest = strings.Repeat("b", 64)
	changed.Network.ScopeDigest = network.ScopeDigest(changed.Network)
	changedPayload := `{"code":"different opaque script","preload":""}`
	changed.InvocationDigest = InvocationDigest(changed, []byte(changedPayload))
	if _, err = sup.Start(context.Background(), changed, strings.NewReader(changedPayload)); err == nil {
		t.Fatal("client digest recomputation minted approval")
	}
	if _, err = sup.Lookup(changed.ExecutionID, changed.InvocationDigest); err == nil {
		t.Fatal("unauthorized execution acquired identity")
	}
	entries, err := os.ReadDir(p.WorkRoot)
	if err != nil || len(entries) != 0 {
		t.Fatal("unauthorized execution allocated root")
	}
}
func TestUIDLeaseRetainedWhenFinalReleaseCannotProveAbsence(t *testing.T) {
	root := t.TempDir()
	pool, err := uidpool.NewPersistentUIDPool(20000, 20001, filepath.Join(root, "leases"))
	if err != nil {
		t.Fatal(err)
	}
	uid, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	resource := filepath.Join(root, "residual")
	if err = pool.TrackResource(uid, resource); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(resource, 0700); err != nil {
		t.Fatal(err)
	}
	defer func() { os.Remove(resource); pool.ReleaseChecked(uid) }()
	released, quarantined := false, false
	p := testProfile(t)
	p.HostUIDMin, p.HostUIDMax = 20000, 20001
	j, err := openJournal(p)
	if err != nil {
		t.Fatal(err)
	}
	defer j.close()
	e := &Execution{spec: testSpec(p), journal: j, done: make(chan struct{}), hostUID: uid, uidPool: pool, result: ExecutionResult{CleanupState: "clean"}, release: func() { released = true }, quarantine: func() { quarantined = true }}
	e.spec.InvocationDigest = strings.Repeat("a", 64)
	e.complete("succeeded", "")
	if e.result.Status != "unknown" || e.result.CleanupState != "quarantined" || released || !quarantined || pool.Len() != 0 {
		t.Fatalf("uncertain UID was recycled: %+v", e.result)
	}
}

func TestWorkerStartTransfersFrontendOwnership(t *testing.T) {
	if os.Getenv("CS4_START_CHILD") == "1" {
		f := os.NewFile(3, "frontend")
		b := make([]byte, 1)
		if _, err := f.Read(b); err != nil || b[0] != 7 {
			os.Exit(2)
		}
		f.Close()
		os.Stdout.Write([]byte("closed\n"))
		time.Sleep(2 * time.Second)
		os.Exit(0)
	}
	parent, child, err := network.NewChannel()
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	defer child.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestWorkerStartTransfersFrontendOwnership$")
	cmd.Env = append(os.Environ(), "CS4_START_CHILD=1")
	cmd.ExtraFiles = []*os.File{child}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = startWorker(cmd, child); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	if _, err = parent.Write([]byte{7}); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 7)
	if _, err = io.ReadFull(out, b); err != nil || string(b) != "closed\n" {
		t.Fatalf("inherited endpoint unavailable: %q %v", b, err)
	}
	if _, err = child.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("parent retained frontend after worker start: %v", err)
	}
}
func TestFailedWorkerStartRetainsFrontendForCallerCleanup(t *testing.T) {
	parent, child, err := network.NewChannel()
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	defer child.Close()
	cmd := exec.Command("/nonexistent-cs4-worker")
	cmd.ExtraFiles = []*os.File{child}
	if err = startWorker(cmd, child); err == nil {
		t.Fatal("missing worker started")
	}
	if _, err = child.Stat(); err != nil {
		t.Fatal("failed-start endpoint ownership lost")
	}
}
