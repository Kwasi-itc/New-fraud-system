package app

import (
	"strings"
	"testing"
)

func TestScreeningIntegrationCredentialConfiguration(t *testing.T) {
	a, b := strings.Repeat("a", 32), strings.Repeat("b", 32)
	for _, tc := range []struct {
		i, c, g, o string
		ok         bool
	}{
		{"", "", "general", "operator", true}, {a, b, "general", "operator", true},
		{"short", b, "general", "operator", false}, {a, a, "general", "operator", false},
		{a, b, a, "operator", false}, {a, b, "general", b, false},
	} {
		err := validateScreeningServiceTokens(tc.i, tc.c, tc.g, tc.o)
		if (err == nil) != tc.ok {
			t.Fatal("configuration result", err)
		}
	}
}
