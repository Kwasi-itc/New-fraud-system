package localdev

import (
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/ec2test"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalOriginRequiresExplicitLoopbackConfiguration(t *testing.T) {
	for _, tc := range [][2]string{{"", "http://localhost:3001"}, {"true", "http://localhost:3001"}, {"1", "https://example.com"}, {"1", "http://0.0.0.0:3001"}, {"1", "http://localhost.example.com"}, {"1", "http://user@localhost"}, {"1", "http://localhost/path"}} {
		if _, err := Origin(tc[0], tc[1]); err == nil {
			t.Fatalf("accepted unsafe configuration: %v", tc)
		}
	}
}

func TestEC2BoundaryRequiresActualServiceCredential(t *testing.T) {
	const origin = "http://192.0.2.10:8080"
	const credential = "ec2-test-service-credential-long-enough"
	path := filepath.Join(t.TempDir(), "ec2.json")
	raw, err := json.Marshal(ec2test.Config{Address: "writer.rds.amazonaws.com:3306", AdminUser: "admin", CAFile: "unused-in-http-handler", AccountHost: "10.0.1.10/255.255.255.255", Origins: []string{origin}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ec2test.Environment, path)
	calls := 0
	h, err := WithoutLogin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer "+credential {
			t.Fatal("owner identity changed")
		}
		w.WriteHeader(204)
	}), "1", origin, credential)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		token  string
		status int
	}{{"", 401}, {"Bearer wrong", 401}, {"Bearer " + credential, 204}} {
		r := httptest.NewRequest("POST", "/v1/tenants", nil)
		r.Header.Set("Authorization", tc.token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("status %d, want %d", w.Code, tc.status)
		}
	}
	if calls != 1 {
		t.Fatal("unauthorized request reached owner")
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
