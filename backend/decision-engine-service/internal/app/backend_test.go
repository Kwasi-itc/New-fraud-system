package app

import (
	"errors"
	"testing"
)

func TestRuntimeBackendCannotSilentlyActivateMySQL(t *testing.T) {
	if err := (Config{DatabaseBackend: "mysql"}).ValidateRuntimeBackend(); !errors.Is(err, ErrMySQLActivationBlocked) {
		t.Fatal(err)
	}
	if _, err := New(Config{DatabaseBackend: "mysql"}, nil); !errors.Is(err, ErrMySQLActivationBlocked) {
		t.Fatal("dialed before activation gate", err)
	}
	for _, cfg := range []Config{{DatabaseBackend: "unknown"}, {DatabaseBackend: "postgres", DatabaseURL: "mysql://do-not-dial"}} {
		if err := cfg.ValidateRuntimeBackend(); err == nil {
			t.Fatal("backend accepted", cfg.DatabaseBackend)
		}
	}
	t.Setenv("DATABASE_BACKEND", "mysql")
	if _, err := LoadConfig(); !errors.Is(err, ErrMySQLActivationBlocked) {
		t.Fatal("legacy config validation before backend gate", err)
	}
}
