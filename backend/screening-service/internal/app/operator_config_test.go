package app

import (
	"strings"
	"testing"
)

func TestRecoveryOperatorConfiguration(t *testing.T) {
	secret := strings.Repeat("p", 32)
	for _, tc := range []struct {
		token, service string
		valid          bool
	}{
		{"", "general", true}, {"short", "general", false}, {strings.Repeat(" ", 32), "general", false},
		{secret, secret, false}, {secret, "general", true},
	} {
		err := validateOperatorToken(tc.token, tc.service)
		if (err == nil) != tc.valid {
			t.Fatal("unexpected operator configuration result")
		}
		if err != nil && tc.token != "" && strings.Contains(err.Error(), tc.token) {
			t.Fatal("secret in configuration error")
		}
	}
}
