package app

import (
	"strings"
	"testing"
)

func TestScreeningOperatorCredentialConfiguration(t *testing.T) {
	setRequiredConfigEnv(t)
	for _, tc := range []struct {
		name, credential, service string
		valid                     bool
	}{
		{"absent disables operator route", "", "legacy", true},
		{"short", "short", "legacy", false},
		{"blank", strings.Repeat(" ", 32), "legacy", false},
		{"reused legacy credential", strings.Repeat("x", 32), strings.Repeat("x", 32), false},
		{"separate private credential", strings.Repeat("x", 32), "legacy", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SCREENING_AUTHORING_TOKEN", tc.credential)
			t.Setenv("SERVICE_AUTH_TOKEN", tc.service)
			cfg, err := LoadConfig()
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
			if err == nil && cfg.ScreeningAuthoringToken != tc.credential {
				t.Fatal("credential not loaded")
			}
		})
	}
}
