package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
)

func TestManualAdvancedProviderSerialization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Queries map[string]struct {
				Schema     string              `json:"schema"`
				Properties map[string][]string `json:"properties"`
			} `json:"queries"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		q := body.Queries["q1"]
		if q.Schema != "Person" || len(q.Properties["birthDate"]) != 1 || q.Properties["birthDate"][0] != "1980" || q.Properties["nationality"][0] != "gb" || q.Properties["address"][0] != "London" {
			t.Errorf("wrong payload %+v", q)
		}
		if r.URL.Query().Get("include_dataset") != "us_ofac_sdn" {
			t.Error("dataset not sent")
		}
		_, _ = w.Write([]byte(`{"responses":{"q1":{"total":{"value":0},"results":[]}}}`))
	}))
	defer server.Close()
	client := NewHTTPClient("", nil, time.Second, OpenSanctionsConfig{APIHost: server.URL})
	_, err := client.Search(context.Background(), screening.SearchRequest{Provider: "opensanctions", Queries: []screening.SearchQuery{{Name: "Example", Type: "Person", BirthDate: "1980", Nationality: "gb", Address: "London"}}, ProviderConfig: json.RawMessage(`{"datasets":["us_ofac_sdn"]}`)})
	if err != nil {
		t.Fatal(err)
	}
}

func TestManualProviderFailureContracts(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		wantError bool
	}{
		{"no matches", 200, `{"responses":{"q1":{"total":{"value":0},"results":[]}}}`, false},
		{"rate limited", 429, `{"error":"rate limited"}`, true},
		{"unavailable", 503, `{"error":"unavailable"}`, true},
		{"malformed", 200, `{broken`, true},
		{"missing responses", 200, `{}`, true},
		{"missing query", 200, `{"responses":{}}`, true},
		{"missing results", 200, `{"responses":{"q1":{}}}`, true},
		{"missing candidate id", 200, `{"responses":{"q1":{"results":[{"match":true,"score":0.8}]}}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client := NewHTTPClient("", nil, time.Second, OpenSanctionsConfig{APIHost: server.URL})
			result, err := client.Search(context.Background(), screening.SearchRequest{Provider: "opensanctions", Queries: []screening.SearchQuery{{Name: "Example", Type: "Organization"}}})
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v expected error=%v", err, tc.wantError)
			}
			if !tc.wantError && (len(result.Matches) != 0 || result.Partial) {
				t.Fatalf("unexpected empty result: %+v", result)
			}
		})
	}
}

func TestManualProviderDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Bound fixture shutdown even if the server has not observed the client's
		// disconnect while the request body is unread.
		select {
		case <-r.Context().Done():
		case <-time.After(200 * time.Millisecond):
		}
	}))
	defer server.Close()
	client := NewHTTPClient("", nil, 30*time.Millisecond, OpenSanctionsConfig{APIHost: server.URL})
	_, err := client.Search(context.Background(), screening.SearchRequest{Provider: "opensanctions", Queries: []screening.SearchQuery{{Name: "Example", Type: "Person"}}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected bounded deadline, got %v", err)
	}
}

func TestManualAdvancedRejectsUnverifiedGenericProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unsupported request reached generic provider") }))
	defer server.Close()
	client := NewHTTPClient(server.URL, nil, time.Second, OpenSanctionsConfig{})
	if _, err := client.Search(context.Background(), screening.SearchRequest{Provider: "opensanctions", IsManual: true, Queries: []screening.SearchQuery{{Name: "Example", Type: "Person", BirthDate: "1980"}}}); err == nil {
		t.Fatal("unsupported fields silently accepted")
	}
}
