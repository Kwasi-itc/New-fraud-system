package mysqlruntime

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocalAuthorBoundary(t *testing.T) {
	secret := strings.Repeat("a", 64)
	handler, err := NewLocalAuthorHTTP(&LocalCreation{Secret: secret})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, path, token, key, body string
		status                       int
	}{
		{"no_token", "11111111-1111-4111-8111-111111111111", "", "", `{"name":"transactions"}`, 403},
		{"wrong_token", "11111111-1111-4111-8111-111111111111", "wrong", "", `{"name":"transactions"}`, 403},
		{"invalid_tenant", "invalid", secret, "", `{"name":"transactions"}`, 400},
		{"zero_tenant", "00000000-0000-0000-0000-000000000000", secret, "", `{"name":"transactions"}`, 400},
		{"invalid_body", "11111111-1111-4111-8111-111111111111", secret, "", `{`, 400},
		{"invalid_operation", "11111111-1111-4111-8111-111111111111", secret, "bad", `{"name":"transactions"}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/v1/tenants/"+tc.path+"/tables", strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer "+tc.token)
			req.Header.Set("Idempotency-Key", tc.key)
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != tc.status {
				t.Fatalf("%d: %s", res.Code, res.Body.String())
			}
		})
	}
}
