package app

import "testing"

func TestLoadConfigRequiresAuthTokenForTokenMode(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("SERVICE_AUTH_MODE", "token")
	t.Setenv("SERVICE_AUTH_TOKEN", "")

	_, err := LoadConfig()
	if err == nil {
		t.Fatal("expected missing auth token error")
	}
}

func TestLoadConfigAcceptsDisabledAuthWithoutToken(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("SERVICE_AUTH_MODE", "disabled")
	t.Setenv("SERVICE_AUTH_TOKEN", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ServiceAuthMode != "disabled" {
		t.Fatalf("unexpected auth mode: %s", cfg.ServiceAuthMode)
	}
}
