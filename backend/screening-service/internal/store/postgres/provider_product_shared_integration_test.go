package postgres_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/internalprovider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/providerproduct"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/google/uuid"
)

func TestProviderProductHTTPFraudWorkflowAndInlineFailureAtomicity(t *testing.T) {
	ctx := context.Background()
	var provider *httptest.Server
	var calls atomic.Int64
	var clientID, keyID uuid.UUID
	h := newAcceptanceHarness(t, 0, func(f internalFixture) []string {
		creds := providerproduct.Credentials{DB: f.db}
		var e error
		clientID, e = creds.Create(ctx, providerproduct.ClientInput{Name: "Fraud platform integration fixture", Namespace: f.a.Tenant(), Operations: []string{"search", "enrich", "catalog", "freshness", "delta"}, RequestsPerMinute: 10000, ExpiresAt: time.Now().Add(time.Hour)})
		if e != nil {
			t.Fatal(e)
		}
		var token string
		keyID, e = creds.Issue(ctx, clientID, time.Hour, func(v string) error { token = v; return nil })
		if e != nil {
			t.Fatal(e)
		}
		for _, ref := range []sourcedata.DatasetRef{{Scope: "global", ID: f.global.Dataset}, {Scope: "tenant", ID: f.private.Dataset}} {
			if e = creds.Grant(ctx, clientID, ref, true); e != nil {
				t.Fatal(e)
			}
		}
		handler, e := (providerproduct.Server{DB: f.db, Provider: internalprovider.Provider{DB: f.db}, Access: creds, Timeout: 2 * time.Second, MaxConcurrent: 8}).Handler()
		if e != nil {
			t.Fatal(e)
		}
		provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/v1/") {
				calls.Add(1)
				if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer spk_") {
					t.Error("platform token was sent to provider instead of provider client credential")
				}
			}
			handler.ServeHTTP(w, r)
		}))
		t.Cleanup(provider.Close)
		ring, _ := json.Marshal(providerproduct.Keyring{Version: 1, Clients: []providerproduct.KeyringEntry{{Namespace: f.a.Tenant(), Credential: token}}})
		file := filepath.Join(t.TempDir(), "provider-clients.json")
		if e = os.WriteFile(file, ring, 0600); e != nil {
			t.Fatal(e)
		}
		return []string{"INTERNAL_PROVIDER_TRANSPORT=http", "PROVIDER_HTTP_URL=" + provider.URL, "PROVIDER_CLIENT_KEYS_FILE=" + file}
	})
	tenant := h.f.a.Tenant().String()
	endpoint := h.decisionURL + "/v1/tenants/" + tenant + "/scenarios/" + h.scenario + "/evaluate"
	body := func() map[string]any {
		return map[string]any{"object_id": uuid.NewString(), "object_type": "transaction", "fields": map[string]any{"beneficiary_name": "Ama Testperson", "amount": 500, "country": "GH"}}
	}
	status, raw, e := h.request("POST", endpoint, h.token, body())
	if e != nil || status != 200 {
		t.Fatalf("HTTP-provider decision %d %v %s", status, e, raw)
	}
	var result struct {
		Result struct{ Decision struct{ ID, Outcome string } }
	}
	if json.Unmarshal(raw, &result) != nil || result.Result.Decision.Outcome != "block_and_review" || result.Result.Decision.ID == "" {
		t.Fatal("forced outcome missing")
	}
	var evidence int
	if e = h.f.db.QueryRow(ctx, `SELECT count(*) FROM screening.inline_evidence WHERE decision_id=$1`, result.Result.Decision.ID).Scan(&evidence); e != nil || evidence != 1 {
		t.Fatal("HTTP-provider evidence missing", e)
	}
	input := map[string]any{"operation_id": uuid.NewString(), "provider": sourcedata.Provider, "profile": sourcedata.Profile, "dataset_selection": []sourcedata.DatasetRef{{Scope: "tenant", ID: h.f.private.Dataset}}, "name": "Ama Fixture Alias", "entity_type": "Person", "threshold": 100, "limit": 10}
	status, raw, e = h.request("POST", h.screeningURL+"/v1/manual-screenings", h.human, input)
	if e != nil || status != 202 {
		t.Fatalf("HTTP-provider manual %d %v %s", status, e, raw)
	}
	var saved struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(raw, &saved) != nil || saved.ID == "" {
		t.Fatal("manual ID missing")
	}
	waitFor(t, func() bool {
		var n int
		return h.f.db.QueryRow(ctx, `SELECT count(*) FROM screening.screenings WHERE id=$1 AND completed_at IS NOT NULL AND NOT partial`, saved.ID).Scan(&n) == nil && n == 1
	})
	status, raw, e = h.request("GET", h.screeningURL+"/v1/manual-screenings/"+saved.ID, h.human, nil)
	if e != nil || status != 200 || !strings.Contains(string(raw), "Ama Testperson") {
		t.Fatal("HTTP-worker match missing", status, e)
	}
	if calls.Load() < 2 {
		t.Fatal("integration did not use provider HTTP")
	}
	noHit := body()
	noHit["fields"].(map[string]any)["beneficiary_name"] = "No Candidate " + uuid.NewString()
	status, raw, e = h.request("POST", endpoint, h.token, noHit)
	if e != nil || status != 200 {
		t.Fatalf("complete HTTP no-hit %d %v", status, e)
	}
	if json.Unmarshal(raw, &result) != nil || result.Result.Decision.Outcome != "approve" {
		t.Fatal("complete remote no-hit was not evaluated correctly")
	}
	waitFor(t, func() bool {
		var pending int
		return h.f.db.QueryRow(ctx, `SELECT(SELECT count(*) FROM core.workflow_executions WHERE status<>'dispatched')+(SELECT count(*) FROM core.outbox_events WHERE status<>'sent')`).Scan(&pending) == nil && pending == 0
	})
	var before int
	if e = h.f.db.QueryRow(ctx, `SELECT count(*) FROM core.decisions`).Scan(&before); e != nil {
		t.Fatal(e)
	}
	if e = (providerproduct.Credentials{DB: h.f.db}).Revoke(ctx, clientID, keyID); e != nil {
		t.Fatal(e)
	}
	status, _, e = h.request("POST", endpoint, h.token, body())
	if e != nil || status < 400 {
		t.Fatal("revoked provider credential became a successful decision", status, e)
	}
	provider.Close()
	status, _, e = h.request("POST", endpoint, h.token, body())
	if e != nil || status < 400 {
		t.Fatal("HTTP outage fell back to local matching", status, e)
	}
	var after int
	if e = h.f.db.QueryRow(ctx, `SELECT count(*) FROM core.decisions`).Scan(&after); e != nil || after != before {
		t.Fatal("failed remote evaluation saved a decision", before, after, e)
	}
	acceptanceReport(t, "provider-product-shared-journey", map[string]any{"actual_decision_api": true, "actual_screening_api_and_worker": true, "provider_http_calls": calls.Load(), "forced_outcome_and_evidence": true, "revoked_key_denied": true, "outage_has_no_local_fallback": true, "failed_evaluation_has_no_successful_decision": true, "topology": "shared logical PostgreSQL; separate provider HTTP boundary", "production_activated": false})
}
