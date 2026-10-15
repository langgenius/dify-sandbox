package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/langgenius/dify-sandbox/internal/static"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLifecycleRoutesRequireServerAuthentication(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yaml")
	if err := os.WriteFile(path, []byte("app:\n  key: server-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := static.InitConfig(path); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	SetupBuilder(router, nil)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		route := "/v1/sandbox/executions/opaque-id"
		if method == http.MethodPost {
			route += "/cancel"
		}
		req := httptest.NewRequest(method, route, nil)
		req.Header.Set("X-Invocation-Digest", strings.Repeat("a", 64))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != 401 {
			t.Fatalf("ID/digest bypassed authentication: %d", response.Code)
		}
		req.Header.Set("X-Api-Key", "server-key")
		response = httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != 503 {
			t.Fatalf("authenticated route absent: %d", response.Code)
		}
	}
}
