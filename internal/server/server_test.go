package server

import (
	"strings"
	"testing"
	"time"

	"github.com/langgenius/dify-sandbox/internal/types"
)

func TestPythonDependenciesUpdateInterval(t *testing.T) {
	tests := []struct {
		name           string
		enabled        bool
		interval       string
		wantDuration   time.Duration
		wantEnabled    bool
		wantErr        bool
		wantErrContent string
	}{
		{
			name:        "disabled",
			enabled:     false,
			interval:    "not-a-duration",
			wantEnabled: false,
		},
		{
			name:           "invalid interval",
			enabled:        true,
			interval:       "not-a-duration",
			wantErr:        true,
			wantErrContent: "parse python dependencies update interval",
		},
		{
			name:           "zero interval",
			enabled:        true,
			interval:       "0",
			wantErr:        true,
			wantErrContent: "must be greater than 0",
		},
		{
			name:         "valid interval",
			enabled:      true,
			interval:     "30m",
			wantDuration: 30 * time.Minute,
			wantEnabled:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := types.DifySandboxGlobalConfigurations{
				EnablePythonDepsPeriodicUpdate: tt.enabled,
				PythonDepsUpdateInterval:       tt.interval,
			}

			duration, enabled, err := pythonDependenciesUpdateInterval(config)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				if !strings.Contains(err.Error(), tt.wantErrContent) {
					t.Fatalf("expected error to contain %q, got %q", tt.wantErrContent, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if enabled != tt.wantEnabled {
				t.Fatalf("expected enabled=%v, got %v", tt.wantEnabled, enabled)
			}
			if duration != tt.wantDuration {
				t.Fatalf("expected duration %v, got %v", tt.wantDuration, duration)
			}
		})
	}
}
