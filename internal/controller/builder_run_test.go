package controller

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/langgenius/dify-sandbox/internal/core/runner/environment"
	"github.com/langgenius/dify-sandbox/internal/core/runner/isolation"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuilderControllerRefusesLegacyAndMalformedEnvelopes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// A nil supervisor is an explicitly unconfigured Builder endpoint, never a
	// reason to route to ordinary execution.
	r := gin.New()
	r.POST("/v1/sandbox/run", NewBuilderRunController(nil))
	for _, body := range []string{`{"language":"python3","code":"print(1)"}`, `{"options":{"schema_version":1,"extra":true}}`} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/sandbox/run", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != 503 {
			t.Fatalf("unconfigured Builder accepted request: %d %s", w.Code, w.Body.String())
		}
	}
}
func TestBuilderControllerRejectsMissingEnvelopeWithConfiguredSupervisor(t *testing.T) {
	b := isolation.Budget{InputBytes: 8192, CodeBytes: 4096, StdoutBytes: 1024, StderrBytes: 1024, DiskBytes: 1 << 20, MemoryBytes: 64 << 20, PIDs: 32, FDs: 32, Concurrent: 1, QueueDepth: 2, CPUQuotaMicros: 10000, CPUPeriodMicros: 100000, CPUTimeMillis: 1000, WallMillis: 2000, QueueMillis: 100, CleanupMillis: 100}
	p := isolation.Profile{Digest: strings.Repeat("a", 64), EnvironmentDigest: strings.Repeat("b", 64), EnvironmentRoot: t.TempDir(), WorkRoot: t.TempDir(), CgroupRoot: t.TempDir(), WorkerPath: "/missing-worker", Languages: []string{"python3"}, Limits: b, MaxConcurrent: 1, MaxQueueDepth: 1, MaxQueuedBytes: 32768, MaxExecutions: 5, HostUIDMin: 21000, HostUIDMax: 21016}
	m, err := environment.BuildManifest(p.EnvironmentRoot, environment.EnvironmentManifest{PythonVersion: "fixture", NodeVersion: "fixture", JinjaVersion: "fixture", Architecture: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	if err = environment.WriteManifest(p.EnvironmentRoot, m); err != nil {
		t.Fatal(err)
	}
	p.EnvironmentDigest = m.Digest
	sup, err := isolation.NewSupervisor(p, func(context.Context, isolation.InvocationSpec) (isolation.Budget, error) {
		t.Error("reservation consulted for malformed request")
		return b, nil
	}, func(isolation.InvocationSpec, isolation.Payload) (isolation.Launch, error) {
		t.Error("native runner called")
		return isolation.Launch{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.POST("/v1/sandbox/run", NewBuilderRunController(sup))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/v1/sandbox/run", strings.NewReader(`{"language":"python3","code":"print(1)"}`)))
	if w.Code != 400 {
		t.Fatalf("expected refusal, got %d", w.Code)
	}
}
