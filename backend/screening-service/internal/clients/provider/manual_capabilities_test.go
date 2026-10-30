package provider

import (
	"context"
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestManualCapabilities(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		base                 string
		cfg                  OpenSanctionsConfig
		configured, advanced bool
	}{
		{name: "missing"}, {name: "generic", base: "http://private.example", configured: true},
		{name: "direct", cfg: OpenSanctionsConfig{APIHost: "http://private.example", APIKey: "secret"}, configured: true, advanced: true},
		{name: "missing credential", cfg: OpenSanctionsConfig{APIHost: "http://private.example", AuthMode: "bearer"}, advanced: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cap := NewHTTPClient(tc.base, nil, time.Second, tc.cfg).ManualCapabilities()
			if cap.Configured != tc.configured || cap.DatasetSelection != tc.advanced || cap.Health != "unknown" {
				t.Fatalf("incorrect contract: %+v", cap)
			}
			raw, _ := json.Marshal(cap)
			if strings.Contains(string(raw), "private.example") || strings.Contains(string(raw), "secret") {
				t.Fatal("configuration leaked")
			}
		})
	}
}

func TestProviderErrorRedaction(t *testing.T) {
	for _, direct := range []bool{true, false} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(401)
			_, _ = w.Write([]byte("secret-key Synthetic Person"))
		}))
		cfg := OpenSanctionsConfig{}
		base := server.URL
		if direct {
			cfg = OpenSanctionsConfig{APIHost: server.URL, APIKey: "secret-key"}
			base = ""
		}
		_, err := NewHTTPClient(base, nil, time.Second, cfg).Search(context.Background(), screening.SearchRequest{Provider: "opensanctions", Queries: []screening.SearchQuery{{Name: "Synthetic Person"}}})
		server.Close()
		if err == nil || !strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "Synthetic") || strings.Contains(err.Error(), server.URL) {
			t.Fatalf("unsafe error %v", err)
		}
	}
}

func TestOrganizationFieldSerialization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Queries map[string]struct {
				Schema     string              `json:"schema"`
				Properties map[string][]string `json:"properties"`
			} `json:"queries"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		q := body.Queries["q1"]
		for key, want := range map[string]string{"country": "gb", "registrationNumber": "TEST-123", "address": "Test address"} {
			if len(q.Properties[key]) != 1 || q.Properties[key][0] != want {
				t.Errorf("missing %s", key)
			}
		}
		if q.Schema != "Organization" {
			t.Error("wrong schema")
		}
		_, _ = w.Write([]byte(`{"responses":{"q1":{"total":{"value":0},"results":[]}}}`))
	}))
	defer server.Close()
	_, err := NewHTTPClient("", nil, time.Second, OpenSanctionsConfig{APIHost: server.URL}).Search(context.Background(), screening.SearchRequest{Provider: "opensanctions", Queries: []screening.SearchQuery{{Name: "Synthetic Ltd", Type: "Organization", Country: "gb", RegistrationNumber: "TEST-123", Address: "Test address"}}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGenericRejectsScenarioAddressBeforeHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("unsupported field reached provider") }))
	defer server.Close()
	_, err := NewHTTPClient(server.URL, nil, time.Second, OpenSanctionsConfig{}).Search(context.Background(), screening.SearchRequest{Provider: "opensanctions", Queries: []screening.SearchQuery{{Name: "Synthetic", Type: "Person", Address: "Synthetic Street"}}})
	if err == nil {
		t.Fatal("unsupported address silently accepted")
	}
}

func TestGenericRejectsScenarioBirthDateBeforeHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("unsupported birth date reached provider") }))
	defer server.Close()
	_, err := NewHTTPClient(server.URL, nil, time.Second, OpenSanctionsConfig{}).Search(context.Background(), screening.SearchRequest{Provider: "opensanctions", Queries: []screening.SearchQuery{{Name: "Synthetic", Type: "Person", BirthDate: "1980"}}})
	if err == nil {
		t.Fatal("birth date silently accepted")
	}
}

func TestGenericRejectsScenarioCountryMappingsBeforeHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("unsupported country identity reached provider") }))
	defer server.Close()
	for _, q := range []screening.SearchQuery{{Name: "Synthetic", Type: "Person", Nationality: "gb"}, {Name: "Synthetic", Type: "Organization", Country: "us"}} {
		if _, err := NewHTTPClient(server.URL, nil, time.Second, OpenSanctionsConfig{}).Search(context.Background(), screening.SearchRequest{Provider: "opensanctions", Queries: []screening.SearchQuery{q}}); err == nil {
			t.Fatal("country mapping silently accepted")
		}
	}
}

func TestGenericRejectsScenarioRegistrationBeforeHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("unsupported registration reached provider") }))
	defer server.Close()
	if _, err := NewHTTPClient(server.URL, nil, time.Second, OpenSanctionsConfig{}).Search(context.Background(), screening.SearchRequest{Provider: "opensanctions", Queries: []screening.SearchQuery{{Name: "Synthetic", Type: "Organization", RegistrationNumber: "001-Ab/09"}}}); err == nil {
		t.Fatal("registration silently accepted")
	}
}

func TestGenericRejectsScenarioDatasetsBeforeHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("unsupported dataset filter reached provider") }))
	defer server.Close()
	if _, err := NewHTTPClient(server.URL, nil, time.Second, OpenSanctionsConfig{}).Search(context.Background(), screening.SearchRequest{Provider: "opensanctions", ProviderConfig: []byte(`{"datasets":["synthetic"]}`), Queries: []screening.SearchQuery{{Name: "Synthetic", Type: "Person"}}}); err == nil {
		t.Fatal("unsupported dataset filter accepted")
	}
}

func TestGenericRejectsRecognitionAdapterDrift(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("direct-only recognition reached generic adapter")
	}))
	defer server.Close()
	if _, err := NewHTTPClient(server.URL, nil, time.Second, OpenSanctionsConfig{}).Search(context.Background(), screening.SearchRequest{Provider: "opensanctions", ProviderConfig: []byte(`{"requires_direct_adapter":true}`), Queries: []screening.SearchQuery{{Name: "Synthetic", Type: "Thing"}}}); err == nil {
		t.Fatal("recognition adapter drift accepted")
	}
}
