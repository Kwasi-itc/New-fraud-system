package postgres_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/httpapi"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Case views now join the identity owner for contributor names. Install its
// real schema before exercising case creation; these service-intake tests do
// not need legacy unprovisioned human inbox memberships.
func workflowReceiptFixture(t *testing.T, db *pgxpool.Pool) (service.CaseService, uuid.UUID, casepkg.Inbox, casepkg.Case) {
	t.Helper()
	for _, name := range []string{"schema.sql", "schema_v2.sql", "schema_v3.sql"} {
		raw, err := os.ReadFile("../../../../auth-service/internal/platform/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(context.Background(), string(raw)); err != nil {
			t.Fatal(name, err)
		}
	}
	s := newService(db)
	tenant := uuid.New()
	seedTenant(t, db, tenant)
	inbox, err := s.CreateInbox(adminContext(tenant), service.CreateInboxInput{TenantID: tenant, Name: "Receipt investigations"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateCase(serviceContext(tenant), service.CreateCaseInput{TenantID: tenant, InboxID: inbox.ID, Name: "Initial"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s, tenant, inbox, c
}

func workflowReceiptRequest(t *testing.T, tenant, decision, inbox uuid.UUID) service.WorkflowDispatchRequest {
	t.Helper()
	id := uuid.New()
	payload, err := json.Marshal(map[string]any{"execution_id": id, "tenant_id": tenant, "decision_id": decision, "action_type": "create_case", "action_config": map[string]any{"inbox_id": inbox, "name": "Receipt case"}})
	if err != nil {
		t.Fatal(err)
	}
	return signWorkflowReceipt(t, service.WorkflowDispatchRequest{Kind: "workflow_execution", TenantID: tenant.String(), ResourceID: id.String(), DomainAttempt: 1, Payload: payload})
}
func signWorkflowReceipt(t *testing.T, r service.WorkflowDispatchRequest) service.WorkflowDispatchRequest {
	t.Helper()
	raw, err := json.Marshal(struct {
		Kind                             string
		TenantID, ResourceID             uuid.UUID
		DomainAttempt, RedriveGeneration int
	}{r.Kind, uuid.MustParse(r.TenantID), uuid.MustParse(r.ResourceID), r.DomainAttempt, r.RedriveGeneration})
	if err != nil {
		t.Fatal(err)
	}
	key := sha256.Sum256(raw)
	hash := sha256.Sum256(r.Payload)
	r.Key = "decision-v1-" + hex.EncodeToString(key[:])
	r.PayloadSHA256 = hex.EncodeToString(hash[:])
	return r
}
func TestPostgresWorkflowReceiptOwner(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, inbox, _ := workflowReceiptFixture(t, db)
	decision := seedDecision(t, db, tenant)
	ctx := serviceContext(tenant)
	r := workflowReceiptRequest(t, tenant, decision, inbox.ID)
	before := count(t, db, "cases")
	if got, err := s.ReconcileWorkflowReceipt(ctx, r); err != nil || got.Outcome != "not_found" {
		t.Fatal(got, err)
	}
	t.Run("concurrent_replay_and_lost_response", func(t *testing.T) {
		var wg sync.WaitGroup
		results := make(chan service.WorkflowDispatchReceipt, 8)
		failures := make(chan error, 8)
		start := make(chan struct{})
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				got, err := s.DeliverWorkflowReceipt(ctx, r)
				results <- got
				failures <- err
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		close(failures)
		for err := range failures {
			if err != nil {
				t.Fatal(err)
			}
		}
		var reference string
		for got := range results {
			if got.Outcome != "accepted" || got.Key != r.Key || got.PayloadSHA256 != r.PayloadSHA256 || got.Reference == "" {
				t.Fatal(got)
			}
			if reference != "" && reference != got.Reference {
				t.Fatal("duplicate case")
			}
			reference = got.Reference
		}
		got, err := s.ReconcileWorkflowReceipt(ctx, r)
		if err != nil || got.Reference != reference {
			t.Fatal("lost response", got, err)
		}
		if count(t, db, "cases") != before+1 || count(t, db, "case_decisions") != 1 || count(t, db, "intake_receipts") != 3 {
			t.Fatal("duplicate/missing business or receipt writes")
		}
	})
	t.Run("new_attempt_and_changed_body", func(t *testing.T) {
		original, err := s.ReconcileWorkflowReceipt(ctx, r)
		if err != nil {
			t.Fatal(err)
		}
		next := r
		next.DomainAttempt++
		next.RedriveGeneration++
		next = signWorkflowReceipt(t, next)
		got, err := s.DeliverWorkflowReceipt(ctx, next)
		if err != nil || got.Reference != original.Reference || count(t, db, "cases") != before+1 {
			t.Fatal(got, err)
		}
		changed := r
		changed.Payload = append(append([]byte{}, r.Payload...), byte(' '))
		changed = signWorkflowReceipt(t, changed)
		if _, err := s.DeliverWorkflowReceipt(ctx, changed); !errors.Is(err, casepkg.ErrConflict) {
			t.Fatal("changed body", err)
		}
		if _, err := s.ReconcileWorkflowReceipt(ctx, changed); !errors.Is(err, casepkg.ErrConflict) {
			t.Fatal("changed receipt", err)
		}
		changed.DomainAttempt = 3
		changed = signWorkflowReceipt(t, changed)
		if _, err := s.DeliverWorkflowReceipt(ctx, changed); !errors.Is(err, casepkg.ErrConflict) {
			t.Fatal("new attempt changed body", err)
		}
	})
	t.Run("authority_and_identity", func(t *testing.T) {
		foreign := r
		foreign.TenantID = uuid.NewString()
		foreign = signWorkflowReceipt(t, foreign)
		if _, err := s.ReconcileWorkflowReceipt(ctx, foreign); !errors.Is(err, casepkg.ErrForbidden) {
			t.Fatal("foreign receipt", err)
		}
		if _, err := s.DeliverWorkflowReceipt(context.Background(), r); !errors.Is(err, casepkg.ErrForbidden) {
			t.Fatal("missing service identity", err)
		}
		invalid := r
		invalid.PayloadSHA256 = "wrong"
		if _, err := s.DeliverWorkflowReceipt(ctx, invalid); err == nil {
			t.Fatal("hash bypass")
		}
		invalid = r
		invalid.Key = "wrong"
		if _, err := s.ReconcileWorkflowReceipt(ctx, invalid); err == nil {
			t.Fatal("key bypass")
		}
	})
	t.Run("late_receipt_failure_rolls_back_business", func(t *testing.T) {
		next := workflowReceiptRequest(t, tenant, seedDecision(t, db, tenant), inbox.ID)
		cases, links, events, receipts := count(t, db, "cases"), count(t, db, "case_decisions"), count(t, db, "case_events"), count(t, db, "intake_receipts")
		_, err := db.Exec(ctx, `CREATE FUNCTION case_manager.reject_workflow_receipt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.source LIKE 'workflow.receipt.v1:%' THEN RAISE EXCEPTION 'injected final receipt failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_workflow_receipt BEFORE INSERT ON case_manager.intake_receipts FOR EACH ROW EXECUTE FUNCTION case_manager.reject_workflow_receipt()`)
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.DeliverWorkflowReceipt(ctx, next)
		if _, dropErr := db.Exec(ctx, `DROP TRIGGER reject_workflow_receipt ON case_manager.intake_receipts; DROP FUNCTION case_manager.reject_workflow_receipt()`); dropErr != nil {
			t.Fatal(dropErr)
		}
		if err == nil || got.Outcome == "accepted" {
			t.Fatal("provisional success", got, err)
		}
		if count(t, db, "cases") != cases || count(t, db, "case_decisions") != links || count(t, db, "case_events") != events || count(t, db, "intake_receipts") != receipts {
			t.Fatal("partial transaction")
		}
		if got, err := s.ReconcileWorkflowReceipt(ctx, next); err != nil || got.Outcome != "not_found" {
			t.Fatal(got, err)
		}
		if _, err := s.DeliverWorkflowReceipt(ctx, next); err != nil {
			t.Fatal("retry after rollback", err)
		}
	})
}

func TestPostgresWorkflowReceiptHTTP(t *testing.T) {
	db := testDatabase(t, true)
	_, tenant, inbox, _ := workflowReceiptFixture(t, db)
	decision := seedDecision(t, db, tenant)
	const token = "case-receipt-integration-token-at-least-32-characters"
	config := httpapi.RouterConfig{AuthMode: "token", AuthToken: token, ServiceTenantIDs: []uuid.UUID{tenant}, EnableWorkflowReceipts: true}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := httpapi.NewRouter(logger, db, config)
	r := workflowReceiptRequest(t, tenant, decision, inbox.ID)
	call := func(operation, auth string, request service.WorkflowDispatchRequest, want int) service.WorkflowDispatchReceipt {
		t.Helper()
		raw, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/internal/v1/workflow-receipts/"+operation, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+auth)
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		if recorder.Code != want {
			t.Fatalf("%s: %d want %d: %s", operation, recorder.Code, want, recorder.Body.String())
		}
		var out service.WorkflowDispatchReceipt
		if want == 200 {
			if err := json.Unmarshal(recorder.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
		}
		return out
	}
	call("deliver", "wrong-token", r, 401)
	if got := call("reconcile", token, r, 200); got.Outcome != "not_found" {
		t.Fatal(got)
	}
	accepted := call("deliver", token, r, 200)
	if accepted.Outcome != "accepted" {
		t.Fatal(accepted)
	}
	if got := call("reconcile", token, r, 200); got != accepted {
		t.Fatal("HTTP reconciliation", got, accepted)
	}
	foreign := r
	foreign.TenantID = uuid.NewString()
	foreign = signWorkflowReceipt(t, foreign)
	call("reconcile", token, foreign, 403)
	config.EnableWorkflowReceipts = false
	router = httpapi.NewRouter(logger, db, config)
	call("deliver", token, r, 404)
}
