package providerproduct_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	identity "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/provideraccess"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/providerproduct"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/google/uuid"
)

func TestProviderHTTPClientRejectsRedirectsMalformedEvidenceAndMissingNamespaces(t *testing.T) {
	namespace := uuid.New()
	scope, e := provideraccess.FromPrincipal(identity.Principal{TenantID: namespace, Subject: uuid.NewString(), SessionID: uuid.New(), AuthzVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	token := "spk_" + uuid.NewString() + "." + base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	ring := providerproduct.Keyring{Version: 1, Clients: []providerproduct.KeyringEntry{{Namespace: namespace, Credential: token}}}
	input := providerproduct.SearchInput{Profile: sourcedata.Profile, Threshold: 100, Datasets: []sourcedata.DatasetRef{{Scope: "global", ID: "fixture"}}, Queries: []screening.SearchQuery{{Name: "Fictional", Type: "Person"}}, Limit: 10}
	request, e := input.Request()
	if e != nil {
		t.Fatal(e)
	}
	var leaked atomic.Int64
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1); w.WriteHeader(200) }))
	defer destination.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer redirect.Close()
	client, e := providerproduct.NewClient(redirect.URL, ring, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = client.Search(context.Background(), scope, request); e == nil || leaked.Load() != 0 {
		t.Fatal("credential-following redirect accepted")
	}
	missing, _ := provideraccess.FromPrincipal(identity.Principal{TenantID: uuid.New(), Subject: uuid.NewString(), SessionID: uuid.New(), AuthzVersion: 1})
	if _, e = client.Search(context.Background(), missing, request); !errors.Is(e, provideraccess.ErrScope) {
		t.Fatal("credential namespace fallback", e)
	}
	generation := uuid.New()
	valid := map[string]any{"contract_version": 2, "provider": sourcedata.Provider, "provider_reference": "fixture-reference", "partial": false, "matches": []any{}, "datasets": []any{map[string]any{"ref": input.Datasets[0], "generation": generation}}}
	for _, tc := range []struct {
		name      string
		change    func(map[string]any)
		wantError bool
	}{{"missing completeness", func(v map[string]any) { delete(v, "partial") }, true}, {"missing matches", func(v map[string]any) { delete(v, "matches") }, true}, {"wrong dataset", func(v map[string]any) { v["datasets"] = []any{} }, true}, {"explicit incomplete", func(v map[string]any) { v["partial"] = true }, false}} {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{}
			for k, v := range valid {
				body[k] = v
			}
			tc.change(body)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer "+token {
					t.Error("wrong credential")
				}
				json.NewEncoder(w).Encode(body)
			}))
			defer server.Close()
			client, e := providerproduct.NewClient(server.URL, ring, time.Second)
			if e != nil {
				t.Fatal(e)
			}
			got, e := client.Search(context.Background(), scope, request)
			if (e != nil) != tc.wantError {
				t.Fatalf("expected error=%v got %v", tc.wantError, e)
			}
			if !tc.wantError && !got.Partial {
				t.Fatal("incomplete became clean")
			}
		})
	}
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(500 * time.Millisecond):
		}
	}))
	defer slow.Close()
	client, e = providerproduct.NewClient(slow.URL, ring, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, e = client.Search(ctx, scope, request); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("caller deadline not preserved", e)
	}
	if _, e = providerproduct.NewClient("https://user:secret@example.com", ring, time.Second); e == nil {
		t.Fatal("credentials in endpoint URL accepted")
	}
	if _, e = providerproduct.NewClient("https://example.com/?token=secret", ring, time.Second); e == nil {
		t.Fatal("query credentials accepted")
	}
	duplicate := ring
	duplicate.Clients = append(duplicate.Clients, duplicate.Clients[0])
	if _, e = providerproduct.NewClient("https://example.com", duplicate, time.Second); e == nil {
		t.Fatal("duplicate namespace credential accepted")
	}
}

func TestProviderTransportConfigurationHasNoImplicitFallback(t *testing.T) {
	for _, key := range []string{"INTERNAL_PROVIDER_TRANSPORT", "PROVIDER_HTTP_URL", "PROVIDER_CLIENT_KEYS_FILE"} {
		t.Setenv(key, "")
	}
	if client, e := providerproduct.FromEnvironment(time.Second); e != nil || client != nil {
		t.Fatal("existing local default changed", e)
	}
	t.Setenv("PROVIDER_HTTP_URL", "http://localhost:8091")
	if _, e := providerproduct.FromEnvironment(time.Second); e == nil {
		t.Fatal("HTTP settings silently ignored")
	}
	t.Setenv("INTERNAL_PROVIDER_TRANSPORT", "http")
	t.Setenv("PROVIDER_CLIENT_KEYS_FILE", "/definitely/not/a/provider-keyring")
	if _, e := providerproduct.FromEnvironment(time.Second); e == nil || strings.Contains(e.Error(), "spk_") {
		t.Fatal("missing keyring did not fail safely", e)
	}
}

func TestProviderHTTPClientRejectsMissingFreshnessAndOversizedResponses(t *testing.T) {
	namespace := uuid.New()
	scope, e := provideraccess.FromPrincipal(identity.Principal{TenantID: namespace, Subject: uuid.NewString(), SessionID: uuid.New(), AuthzVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	ring := providerproduct.Keyring{Version: 1, Clients: []providerproduct.KeyringEntry{{Namespace: namespace, Credential: "spk_" + uuid.NewString() + "." + base64.RawURLEncoding.EncodeToString(make([]byte, 32))}}}
	for _, oversized := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if oversized {
				w.Write([]byte(strings.Repeat("x", 8*1024*1024+1)))
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"contract_version": 2, "provider": sourcedata.Provider, "datasets": []any{}})
		}))
		client, e := providerproduct.NewClient(server.URL, ring, time.Second)
		if e != nil {
			t.Fatal(e)
		}
		_, e = client.GetFreshness(context.Background(), scope, sourcedata.Operation{Provider: sourcedata.Provider, Selection: []sourcedata.DatasetRef{{Scope: "global", ID: "fixture"}}})
		server.Close()
		if e == nil {
			t.Fatal("accepted missing dataset freshness or oversized response", oversized)
		}
	}
}
