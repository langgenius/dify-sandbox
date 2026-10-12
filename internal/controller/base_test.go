package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/langgenius/dify-sandbox/internal/types"
)

func TestBindRequestReturnsBadRequestWhenJSONIsInvalid(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/sandbox/run", strings.NewReader(`{"language"`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	successCalled := false
	BindRequest(ctx, func(req struct {
		Language string `json:"language" binding:"required"`
	}) {
		successCalled = true
	})

	if successCalled {
		t.Fatalf("success callback should not be called when request binding fails")
	}

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}

	var resp types.DifySandboxResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response body: %v", err)
	}

	if resp.Code != -400 {
		t.Fatalf("expected response code -400, got %d", resp.Code)
	}
}
