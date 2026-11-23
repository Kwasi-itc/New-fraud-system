package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/internalprovider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/providerproduct"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
	"github.com/google/uuid"
)

func TestAllPublishedWatchlistsIncludeTheLastDatasetThroughEveryProfileAndHTTP(t *testing.T) {
	f := newInternalFixture(t)
	ctx := context.Background()
	store := sourceingest.Store{DB: f.db}
	refs := []sourcedata.DatasetRef{}
	for i := 0; i < 12; i++ {
		key := sourceingest.Key{Dataset: fmt.Sprintf("all-catalog-%02d", i)}
		if e := store.Register(ctx, key, key.Dataset, []uuid.UUID{f.a.Tenant()}); e != nil {
			t.Fatal(e)
		}
		gen, e := store.Stage(ctx, key, bytes.NewReader(f.input))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = store.Publish(ctx, key, gen); e != nil {
			t.Fatal(e)
		}
		refs = append(refs, sourcedata.DatasetRef{Scope: "global", ID: key.Dataset})
	}
	last := sourceingest.Key{Owner: f.a.Tenant(), Dataset: "last-private-catalog"}
	if e := store.Register(ctx, last, "Last private catalog", []uuid.UUID{f.a.Tenant()}); e != nil {
		t.Fatal(e)
	}
	raw := []byte(`{"version":2,"source":"tenant-upload","parser":"source-json-v2","records":[{"id":"last-record","name":"Fictional Last Catalog Witness","type":"Person","identifiers":[{"type":"passport","value":"000-CATALOG-99","country":"gh"}],"source_reference":"Authored fixture only"}]}`)
	gen, e := store.Stage(ctx, last, bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = store.Publish(ctx, last, gen); e != nil {
		t.Fatal(e)
	}
	lastRef := sourcedata.DatasetRef{Scope: "tenant", ID: last.Dataset}
	refs = append(refs, lastRef)
	provider := internalprovider.Provider{DB: f.db}
	for _, profile := range []string{sourcedata.Profile, sourcedata.FuzzyProfile, sourcedata.TokenProfile, sourcedata.IdentifierProfile} {
		t.Run(profile, func(t *testing.T) {
			selection, _ := json.Marshal(sourcedata.Selection{Version: 2, Profile: profile, Threshold: 85, Datasets: refs})
			q := screening.SearchQuery{Name: "Fictional Last Catalog Witness", Type: "Person"}
			if profile == sourcedata.IdentifierProfile {
				q.Name = "Unrelated Name"
				q.Identifiers = []sourcedata.Identifier{{Type: "passport", Value: "000-CATALOG-99", Country: "gh"}}
			}
			req := screening.SearchRequest{Provider: sourcedata.Provider, ProviderConfig: selection, Queries: []screening.SearchQuery{q}}
			result, e := provider.Search(ctx, f.a, req)
			if e != nil || result.Partial || len(result.Matches) != 1 {
				t.Fatal("last dataset excluded", len(result.Matches), result.Partial, e)
			}
			var proof struct {
				Datasets []internalprovider.Dataset `json:"datasets"`
			}
			if json.Unmarshal(result.RawResponse, &proof) != nil || len(proof.Datasets) != len(refs) {
				t.Fatal("not all selected generations were evaluated")
			}
			if _, e = provider.Search(ctx, f.b, req); e == nil {
				t.Fatal("all-lists selection bypassed tenant grants")
			}
		})
	}
	credentials := providerproduct.Credentials{DB: f.db}
	client, e := credentials.Create(ctx, providerproduct.ClientInput{Name: "All catalogs fixture", Namespace: f.a.Tenant(), Operations: []string{"search", "catalog", "freshness", "delta"}, RequestsPerMinute: 1000, ExpiresAt: time.Now().Add(time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	for _, ref := range refs {
		if e = credentials.Grant(ctx, client, ref, true); e != nil {
			t.Fatal(e)
		}
	}
	var token string
	if _, e = credentials.Issue(ctx, client, time.Hour, func(value string) error { token = value; return nil }); e != nil {
		t.Fatal(e)
	}
	handler, e := (providerproduct.Server{DB: f.db, Provider: provider, Access: credentials, Timeout: 2 * time.Second, MaxConcurrent: 8}).Handler()
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	remote, e := providerproduct.NewClient(server.URL, providerproduct.Keyring{Version: 1, Clients: []providerproduct.KeyringEntry{{Namespace: f.a.Tenant(), Credential: token}}}, 2*time.Second)
	if e != nil {
		t.Fatal(e)
	}
	req := internalRequest("Fictional Last Catalog Witness", "Person", refs...)
	result, e := remote.Search(ctx, f.a, req)
	if e != nil || result.Partial || len(result.Matches) != 1 {
		t.Fatal("HTTP selection narrowed", result, e)
	}
	op := sourcedata.Operation{Provider: sourcedata.Provider, Selection: refs, Limit: 500}
	if _, e = remote.GetCatalog(ctx, f.a, op); e != nil {
		t.Fatal(e)
	}
	if _, e = remote.GetFreshness(ctx, f.a, op); e != nil {
		t.Fatal(e)
	}
	if _, e = remote.GetDatasetDelta(ctx, f.a, op); e != nil {
		t.Fatal(e)
	}
	t.Logf("All %d selected datasets evaluated; last dataset matched in exact/fuzzy/token/identifier profiles and authenticated HTTP; tenant isolation preserved", len(refs))
}
