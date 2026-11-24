package app

import (
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestStartupRequiresSignedAuthenticationEvenWithLegacyOptOut(t *testing.T) {
	for _, mode := range []string{"", "disabled", "token"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("SERVICE_AUTH_MODE", mode)
			t.Setenv("SERVICE_AUTH_TOKEN", "retired-secret")
			t.Setenv("PLATFORM_AUTH_KEYS_FILE", "")
			t.Setenv("PLATFORM_AUTH_AUDIENCE", "")
			_, err := New(Config{GinMode: "test", DatabaseURL: "postgres://must-not-be-contacted.invalid/fraud"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err == nil || !strings.Contains(err.Error(), "PLATFORM_AUTH") {
				t.Fatalf("startup must reject missing trust before database access: %v", err)
			}
			if strings.Contains(err.Error(), "retired-secret") {
				t.Fatal("credential leaked")
			}
		})
	}
}
