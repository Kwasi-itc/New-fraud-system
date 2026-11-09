package workflowreceipt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	casefixture "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/integrationtest"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/testsupport"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestWorkflowReceiptActualOwner(t *testing.T) {
	ctx := context.Background()
	db, err := pgxpool.New(ctx, testsupport.PostgresDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	fixture := casefixture.WorkflowReceiptOwner(t, db, testToken)
	// Commit at the actual owner, then discard its HTTP response. The client must
	// report uncertainty and reconcile rather than treat that transport failure
	// as proof of non-delivery.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/v1/workflow-receipts/deliver" {
			recorder := httptest.NewRecorder()
			fixture.Handler.ServeHTTP(recorder, r)
			if recorder.Code != 200 {
				t.Errorf("owner delivery failed: %d %s", recorder.Code, recorder.Body.String())
			}
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		fixture.Handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, Token: testToken, AllowHTTP: true, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	execution := uuid.New()
	identity := mysqljobs.Identity{Version: 1, Kind: ports.JobWorkflow, TenantID: fixture.Tenant, ResourceID: execution, DomainAttempt: 1}
	key, err := identity.Key()
	if err != nil {
		t.Fatal(err)
	}
	keyBytes, err := json.Marshal(key)
	if err != nil {
		t.Fatal(err)
	}
	keyHash := sha256.Sum256(keyBytes)
	payload, err := json.Marshal(map[string]any{"execution_id": execution, "tenant_id": fixture.Tenant, "decision_id": fixture.Decision, "scenario_id": fixture.Scenario, "action_type": "create_case", "action_config": map[string]any{"inbox_id": fixture.Inbox, "name": "Actual HTTP receipt"}})
	if err != nil {
		t.Fatal(err)
	}
	bodyHash := sha256.Sum256(payload)
	request := ports.DispatchRequest{Key: "decision-v1-" + hex.EncodeToString(keyHash[:]), Kind: ports.JobWorkflow, TenantID: fixture.Tenant.String(), ResourceID: execution.String(), DomainAttempt: 1, PayloadSHA256: hex.EncodeToString(bodyHash[:]), Payload: payload}
	if got, err := client.Deliver(ctx, request); err == nil || got.Outcome == ports.DispatchAccepted {
		t.Fatal("lost response treated as success", got, err)
	}
	receipt, err := client.Reconcile(ctx, request)
	if err != nil || receipt.Outcome != ports.DispatchAccepted || receipt.Key != request.Key || receipt.PayloadSHA256 != request.PayloadSHA256 {
		t.Fatal("reconcile committed owner", receipt, err)
	}
	id, err := uuid.Parse(receipt.Reference)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"cases", "case_decisions"} {
		var count int
		if err := db.QueryRow(ctx, "SELECT COUNT(*) FROM case_manager."+table+" WHERE tenant_id=$1", fixture.Tenant).Scan(&count); err != nil || count != 1 {
			t.Fatal("business outcome", table, count, err)
		}
	}
	var caseID uuid.UUID
	if err := db.QueryRow(ctx, "SELECT case_id FROM case_manager.case_decisions WHERE tenant_id=$1 AND decision_id=$2", fixture.Tenant, fixture.Decision).Scan(&caseID); err != nil || caseID != id {
		t.Fatal("receipt points to wrong case", caseID, err)
	}
}
