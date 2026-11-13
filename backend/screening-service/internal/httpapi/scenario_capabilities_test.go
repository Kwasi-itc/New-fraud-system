package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"testing"
)

func TestScenarioAddressCapabilities(t *testing.T) {
	const secret = "synthetic-intake-private-token-32bytes"
	for _, tc := range []struct {
		name, host, base, auth string
		want                   bool
	}{
		{"direct", "http://private.invalid", "", "", true}, {"generic", "", "http://private.invalid", "", false}, {"missing", "", "", "", false}, {"unconfigured", "http://private.invalid", "", "bearer", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRouter(slog.Default(), nil, RouterConfig{AuthMode: "disabled", ScreeningIntakeToken: secret, OpenSanctionsAPIHost: tc.host, ScreeningProviderURL: tc.base, OpenSanctionsAuthMode: tc.auth})
			for _, token := range []string{"general", secret} {
				req := httptest.NewRequest("GET", "/internal/screening-capabilities", nil)
				req.Header.Set("Authorization", "Bearer "+token)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				if token != secret {
					if w.Code != 401 {
						t.Fatal(w.Code)
					}
					continue
				}
				var got map[string]bool
				if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || len(got) != 6 || got["matching_settings_supported"] != tc.want || got["address_supported"] != tc.want || got["birth_date_supported"] != tc.want || got["nationality_supported"] != tc.want || got["country_supported"] != tc.want || got["registration_number_supported"] != tc.want {
					t.Fatal(w.Code, w.Body.String())
				}
			}
		})
	}
}
