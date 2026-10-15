package isolation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvironmentDriftPreventsLaunch(t *testing.T) {
	p := testProfile(t)
	called := false
	s, err := NewSupervisor(p, func(context.Context, InvocationSpec) (Budget, error) { return p.Limits, nil }, func(InvocationSpec, Payload) (Launch, error) { called = true; return Launch{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = os.WriteFile(filepath.Join(p.EnvironmentRoot, "injected"), []byte("bad"), 0644); err != nil {
		t.Fatal(err)
	}
	spec := testSpec(p)
	payload := `{"code":"print(1)","preload":""}`
	spec.InvocationDigest = InvocationDigest(spec, []byte(payload))
	e, err := s.Start(context.Background(), spec, strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	result, err := e.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if called || result.Status != "configuration_required" || result.FailureReason != "environment_mismatch" {
		t.Fatalf("drift reached launcher: called=%v result=%+v", called, result)
	}
}
func TestSupervisorRejectsUnbakedEnvironment(t *testing.T) {
	p := testProfile(t)
	p.EnvironmentRoot = t.TempDir()
	if s, err := NewSupervisor(p, func(context.Context, InvocationSpec) (Budget, error) { return p.Limits, nil }, func(InvocationSpec, Payload) (Launch, error) { return Launch{}, nil }); err == nil {
		s.Close()
		t.Fatal("unbaked environment accepted")
	}
}
