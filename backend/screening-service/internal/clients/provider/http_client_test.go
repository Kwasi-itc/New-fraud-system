package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
)

func TestOpenSanctionsSearchUsesAPIKeyQueryAndAdaptsMatches(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/match/default" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("api_key"); got != "key-1" {
			t.Fatalf("api_key = %q", got)
		}
		if got := r.URL.Query().Get("algorithm"); got != "logic-v1" {
			t.Fatalf("algorithm = %q", got)
		}
		if got := r.URL.Query().Get("threshold"); got != "0.85" {
			t.Fatalf("threshold = %q", got)
		}
		if got := r.URL.Query().Get("limit"); got != "10" {
			t.Fatalf("limit = %q", got)
		}
		var body map[string]map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if _, ok := body["queries"]["q1"]; !ok {
			t.Fatalf("queries missing q1: %#v", body)
		}
		_, _ = w.Write([]byte(`{
			"responses": {
				"q1": {
					"total": {"value": 2},
					"results": [
						{"id":"ent-1","caption":"John Smith","match":true,"score":0.98,"properties":{"name":["John Smith"]}},
						{"id":"ent-2","caption":"Jane Smith","match":false,"score":0.12,"properties":{"name":["Jane Smith"]}}
					]
				}
			}
		}`))
	}))
	defer server.Close()

	limit := 10
	client := NewHTTPClient("", nil, time.Second, OpenSanctionsConfig{
		APIHost:   server.URL,
		AuthMode:  "saas",
		APIKey:    "key-1",
		Scope:     "default",
		Algorithm: "logic-v1",
	})
	threshold := 85
	providerConfig := json.RawMessage(`{"threshold":85}`)
	result, err := client.Search(context.Background(), screening.SearchRequest{
		Provider:       "opensanctions",
		ObjectType:     "business",
		ObjectID:       "obj-1",
		Queries:        []screening.SearchQuery{{Name: "John Smith", Type: "person"}},
		ProviderConfig: providerConfig,
		LimitOverride:  &limit,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if !result.Partial {
		t.Fatalf("expected partial result")
	}
	if len(result.Matches) != 1 {
		t.Fatalf("match count = %d", len(result.Matches))
	}
	if result.Matches[0].EntityID != "ent-1" {
		t.Fatalf("entity id = %q", result.Matches[0].EntityID)
	}
	if len(result.Matches[0].MatchedTexts) != 1 || result.Matches[0].MatchedTexts[0] != "John Smith" {
		t.Fatalf("matched texts = %#v", result.Matches[0].MatchedTexts)
	}
	if result.Matches[0].Score <= 0 || threshold != 85 {
		t.Fatalf("unexpected score or threshold handling")
	}
}

func TestOpenSanctionsEnrichUsesBearerAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/entities/entity-1" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-1" {
			t.Fatalf("authorization = %q", got)
		}
		_, _ = w.Write([]byte(`{"id":"entity-1"}`))
	}))
	defer server.Close()

	client := NewHTTPClient("", nil, time.Second, OpenSanctionsConfig{
		APIHost:  server.URL,
		AuthMode: "bearer",
		APIKey:   "token-1",
	})
	result, err := client.Enrich(context.Background(), "opensanctions", "entity-1")
	if err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}
	if !strings.Contains(string(result.RawPayload), `"entity-1"`) {
		t.Fatalf("payload = %s", string(result.RawPayload))
	}
}

func TestOpenSanctionsDatasetDeltaUsesCatalogVersionFingerprint(t *testing.T) {
	responses := [][]byte{
		[]byte(`{"datasets":[{"name":"pep","version":"v1"}]}`),
		[]byte(`{"datasets":[{"name":"pep","version":"v2"}]}`),
	}
	call := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/catalog" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_, _ = w.Write(responses[call])
		call++
	}))
	defer server.Close()

	client := NewHTTPClient("", nil, time.Second, OpenSanctionsConfig{APIHost: server.URL})
	first, err := client.GetDatasetDelta(context.Background(), "opensanctions", "")
	if err != nil {
		t.Fatalf("first GetDatasetDelta() error = %v", err)
	}
	if first.Changed {
		t.Fatalf("expected first baseline delta to be unchanged")
	}
	second, err := client.GetDatasetDelta(context.Background(), "opensanctions", first.NextCursor)
	if err != nil {
		t.Fatalf("second GetDatasetDelta() error = %v", err)
	}
	if !second.Changed {
		t.Fatalf("expected changed delta on version update")
	}
}
