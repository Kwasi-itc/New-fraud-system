package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceAuthConfiguration(t *testing.T) {
	// Keep a developer's .env out of the test; an omitted mode must be tested as
	// omitted, not silently replaced by a local development configuration.
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Error(err)
		}
	})
	t.Setenv("DATABASE_URL", "postgres://test.invalid/fraud")
	t.Setenv("DATA_MODEL_SERVICE_URL", "http://data-model.invalid")
	t.Setenv("INGESTION_SERVICE_URL", "http://ingestion.invalid")
	tests := []struct{ name, mode, token, wantMode, wantError string }{
		{"default requires token", "", "", "", "SERVICE_AUTH_TOKEN"},
		{"default with token", "", "test-service-token", "token", ""},
		{"explicit token missing secret", "token", "", "", "SERVICE_AUTH_TOKEN"},
		{"blank secret", "token", " \t ", "", "SERVICE_AUTH_TOKEN"},
		{"unknown mode", "tokne", "test-service-token", "", "SERVICE_AUTH_MODE"},
		{"explicit token", "token", "test-service-token", "token", ""},
		{"explicit local opt out", "disabled", "", "disabled", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SERVICE_AUTH_MODE", tt.mode)
			t.Setenv("SERVICE_AUTH_TOKEN", tt.token)
			cfg, err := LoadConfig()
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("error=%v, want %s configuration error", err, tt.wantError)
				}
				if strings.Contains(err.Error(), "test-service-token") {
					t.Fatal("secret leaked")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.ServiceAuthMode != tt.wantMode {
				t.Fatalf("mode=%q want %q", cfg.ServiceAuthMode, tt.wantMode)
			}
		})
	}
	// Dotenv opt-out remains explicit and supported.
	if err := os.WriteFile(filepath.Join(".", ".env"), []byte("SERVICE_AUTH_MODE=disabled\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SERVICE_AUTH_MODE", "")
	t.Setenv("SERVICE_AUTH_TOKEN", "")
	cfg, err := LoadConfig()
	if err != nil || cfg.ServiceAuthMode != "disabled" {
		t.Fatalf("explicit dotenv opt-out: config=%q error=%v", cfg.ServiceAuthMode, err)
	}
}
