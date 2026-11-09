package mysqlruntime

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocalMutationValidationBeforeDatabaseAccess(t *testing.T) {
	secret := strings.Repeat("a", 64)
	handler, err := NewLocalAuthorHTTP(&LocalCreation{Secret: secret})
	if err != nil {
		t.Fatal(err)
	}
	id := "11111111-1111-4111-8111-111111111111"
	for _, tc := range []struct {
		method, path, body, key string
		status                  int
	}{
		{"PATCH", "/v1/tables/invalid", `{"alias":"x"}`, "", 400},
		{"PATCH", "/v1/tables/" + id, `{"alias":"x","name":"ignored"}`, "", 400},
		{"PATCH", "/v1/tables/" + id, `{"alias":"x"} {}`, "", 400},
		{"PATCH", "/v1/tables/" + id, `{}`, "", 400},
		{"PATCH", "/v1/tables/" + id, `null`, "", 400},
		{"PATCH", "/v1/tables/" + id, `{"alias":"x"}`, "bad", 400},
		{"POST", "/v1/tables/" + id + "/fields", `{"name":"score","data_type":"int"}`, "bad", 400},
	} {
		t.Run(tc.method+tc.path+tc.body, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer "+secret)
			req.Header.Set("Idempotency-Key", tc.key)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != tc.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			req = httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			response = httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != 403 {
				t.Fatalf("unauthenticated mutation: %d", response.Code)
			}
		})
	}
}
