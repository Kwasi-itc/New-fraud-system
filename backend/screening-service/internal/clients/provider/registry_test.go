package provider

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStrictProviderResolutionNeverCallsDefaultForUnknownKey(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.Write([]byte(`{"matches":[]}`)) }))
	defer server.Close()
	client := NewHTTPClient(server.URL, nil, time.Second, OpenSanctionsConfig{})
	for _, key := range []string{"", "unknown", "synthetic", "internal-watchlists"} {
		if _, err := client.Search(context.Background(), screening.SearchRequest{Provider: key}); err == nil {
			t.Fatalf("unknown provider accepted: %q", key)
		}
	}
	if calls != 0 {
		t.Fatal("fallback made network requests", calls)
	}
	if _, err := client.Search(context.Background(), screening.SearchRequest{Provider: "opensanctions"}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("named adapter not used")
	}
}
func TestRegistryConfigurationRejectsAmbiguity(t *testing.T) {
	for _, raw := range []string{`{"one":"http://example.test","one":"http://else.test"}`, `{"synthetic":"http://example.test"}`, `{"internal-watchlists":"http://example.test"}`, `{"one":"file:///etc/passwd"}`, `one=http://example.test,one=http://else.test`, `not-a-map`, `{"one":"http://example.test"} trailing`} {
		if ValidateConfiguration("", raw, "") == nil {
			t.Fatalf("accepted invalid config: %s", raw)
		}
	}
	if err := ValidateConfiguration("http://example.test", `{"named":"https://example.test"}`, ""); err != nil {
		t.Fatal(err)
	}
}
