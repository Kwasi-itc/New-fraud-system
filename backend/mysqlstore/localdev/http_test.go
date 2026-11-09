package localdev

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocalOriginRequiresExplicitLoopbackConfiguration(t *testing.T) {
	for _, tc := range [][2]string{{"", "http://localhost:3001"}, {"true", "http://localhost:3001"}, {"1", "https://example.com"}, {"1", "http://0.0.0.0:3001"}, {"1", "http://localhost.example.com"}, {"1", "http://user@localhost"}, {"1", "http://localhost/path"}} {
		if _, err := Origin(tc[0], tc[1]); err == nil {
			t.Fatalf("accepted unsafe configuration: %v", tc)
		}
	}
}

func TestWithoutLoginUsesServerCredentialAndRejectsForeignBrowsers(t *testing.T) {
	const credential = "local-internal-service-credential"
	calls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer "+credential {
			t.Error("caller selected authority")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	h, err := WithoutLogin(next, "1", "http://localhost:3001", credential)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		origin, site string
		status       int
	}{{"", "", 204}, {"http://localhost:3001", "same-site", 204}, {"http://evil.example", "", 403}, {"", "cross-site", 403}} {
		r := httptest.NewRequest("POST", "/v1/tenants/local/ingest/records", nil)
		r.Header.Set("Authorization", "Bearer caller-supplied")
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Sec-Fetch-Site", tc.site)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("got %d want %d", w.Code, tc.status)
		}
		if r.Header.Get("Authorization") != "Bearer caller-supplied" {
			t.Fatal("mutated caller request")
		}
	}
	if calls != 2 {
		t.Fatalf("unexpected owner calls: %d", calls)
	}
}
