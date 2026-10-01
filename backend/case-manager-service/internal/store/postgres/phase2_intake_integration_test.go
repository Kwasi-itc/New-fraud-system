package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/httpapi"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
	"github.com/google/uuid"
)

func TestPostgresWorkflowHTTPIntake(t *testing.T) {
	db := testDatabase(t, true)
	_, tenant, inbox, _ := fixture(t, db)
	decision := seedDecision(t, db, tenant)
	const token = "test-case-integration-token-at-least-32-characters"
	router := httpapi.NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), db, httpapi.RouterConfig{
		AuthMode: "token", AuthToken: token, ServiceTenantIDs: []uuid.UUID{tenant},
	})
	server := httptest.NewServer(router)
	defer server.Close()
	payload := map[string]any{
		"workflow_execution_id": uuid.NewString(), "tenant_id": tenant, "decision_id": decision,
		"action_type": "create_case", "action_config": map[string]any{"inbox_id": inbox.ID, "name": "HTTP alert"},
	}
	post := func(auth string, expected int) []byte {
		t.Helper()
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(http.MethodPost, server.URL+"/internal/v1/workflow-actions", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+auth)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != expected {
			t.Fatalf("HTTP %d, want %d: %s", resp.StatusCode, expected, data)
		}
		return data
	}
	post("wrong-token", http.StatusUnauthorized)
	if count(t, db, "intake_receipts") != 0 {
		t.Fatal("unauthorized request persisted")
	}
	first := post(token, http.StatusOK)
	second := post(token, http.StatusOK)
	var a, b struct {
		Case casepkg.Case `json:"case"`
	}
	if err := json.Unmarshal(first, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(second, &b); err != nil {
		t.Fatal(err)
	}
	if a.Case.ID == uuid.Nil || a.Case.ID != b.Case.ID || count(t, db, "intake_receipts") != 1 || count(t, db, "case_decisions") != 1 {
		t.Fatal("HTTP replay was not deduplicated")
	}
	payload["action_config"] = map[string]any{"inbox_id": inbox.ID, "name": "Conflicting alert"}
	post(token, http.StatusConflict)
	payload["tenant_id"] = uuid.New()
	post(token, http.StatusForbidden)
}

func TestPostgresWorkflowIntakeRetryAndGrouping(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, inbox, _ := fixture(t, db)
	decision := seedDecision(t, db, tenant)
	ctx := serviceContext(tenant)
	in := service.WorkflowActionInput{WorkflowExecutionID: uuid.New(), TenantID: tenant, DecisionID: decision, ActionType: "add_to_case_if_possible", ActionConfig: service.WorkflowActionConfig{InboxID: inbox.ID, Name: "Alert"}}
	const workers = 12
	results := make(chan casepkg.Case, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); item, err := s.HandleWorkflowAction(ctx, in); results <- item; errs <- err }()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first uuid.UUID
	for item := range results {
		if first == uuid.Nil {
			first = item.ID
		}
		if first != item.ID {
			t.Fatal("retry created different cases")
		}
	}
	if count(t, db, "intake_receipts") != 1 || count(t, db, "cases") != 2 || count(t, db, "case_decisions") != 1 {
		t.Fatal("duplicate intake writes")
	}
	before := count(t, db, "case_events")
	changed := in
	changed.ActionConfig.Name = "Conflicting payload"
	if _, err := s.HandleWorkflowAction(ctx, changed); !errors.Is(err, casepkg.ErrConflict) {
		t.Fatalf("conflicting replay: %v", err)
	}
	// Distinct executions share a grouping lock, including inbox-specific and
	// any-inbox actions for the same object.
	var group sync.WaitGroup
	groupErrs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			next := in
			next.WorkflowExecutionID = uuid.New()
			next.ActionConfig.AnyInbox = i%2 == 0
			item, err := s.HandleWorkflowAction(ctx, next)
			if err == nil && item.ID != first {
				err = errors.New("grouping created a duplicate")
			}
			groupErrs <- err
		}(i)
	}
	group.Wait()
	close(groupErrs)
	for err := range groupErrs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if count(t, db, "cases") != 2 || count(t, db, "intake_receipts") != workers+1 {
		t.Fatal("incorrect grouping receipts")
	}
	if count(t, db, "case_events") != before+workers {
		t.Fatal("unexpected logical action count")
	}
	closed := casepkg.StatusClosed
	if _, err := s.UpdateCase(adminContext(tenant), service.UpdateCaseInput{TenantID: tenant, CaseID: first, Status: &closed}, nil); err != nil {
		t.Fatal(err)
	}
	next := in
	next.WorkflowExecutionID = uuid.New()
	next.ActionType = "add_to_case"
	if _, err := s.HandleWorkflowAction(ctx, next); !errors.Is(err, casepkg.ErrNotFound) {
		t.Fatalf("add-only created a case: %v", err)
	}
	next.ActionType = "add_to_case_if_possible"
	item, err := s.HandleWorkflowAction(ctx, next)
	if err != nil || item.ID == first {
		t.Fatalf("closed case reused: %+v %v", item, err)
	}
}

func TestPostgresWorkflowReceiptFailureRollsBack(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, inbox, _ := fixture(t, db)
	decision := seedDecision(t, db, tenant)
	failInserts(t, db, "intake_receipts")
	in := service.WorkflowActionInput{WorkflowExecutionID: uuid.New(), TenantID: tenant, DecisionID: decision, ActionType: "create_case", ActionConfig: service.WorkflowActionConfig{InboxID: inbox.ID}}
	before := count(t, db, "cases")
	if _, err := s.HandleWorkflowAction(serviceContext(tenant), in); err == nil {
		t.Fatal("receipt failure ignored")
	}
	if count(t, db, "cases") != before || count(t, db, "case_decisions") != 0 {
		t.Fatal("writes escaped receipt rollback")
	}
}

func TestPostgresScreeningIntakePreservesMatchesAndDeduplicates(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, _, c := fixture(t, db)
	decision := seedDecision(t, db, tenant)
	ctx := serviceContext(tenant)
	if _, err := s.AddDecision(ctx, service.AddDecisionInput{TenantID: tenant, CaseID: c.ID, DecisionID: decision}, nil); err != nil {
		t.Fatal(err)
	}
	screening, match, _ := seedScreening(t, db, tenant, decision)
	in := service.ScreeningReviewInput{EventID: uuid.New(), TenantID: tenant, ScreeningID: screening, DecisionID: &decision, MatchID: match, Status: "no_hit"}
	if err := s.ReceiveScreeningReview(ctx, in); err != nil {
		t.Fatal(err)
	}
	before := count(t, db, "case_events")
	if err := s.ReceiveScreeningReview(ctx, in); err != nil {
		t.Fatal(err)
	}
	if count(t, db, "case_events") != before {
		t.Fatal("duplicate review audit")
	}
	changed := in
	changed.Status = "confirmed_hit"
	if err := s.ReceiveScreeningReview(ctx, changed); !errors.Is(err, casepkg.ErrConflict) {
		t.Fatalf("conflicting review: %v", err)
	}
	second := uuid.NewString()
	_, err := db.Exec(context.Background(), `INSERT INTO screening.screening_matches(id,tenant_id,screening_id,entity_id,provider,status,name,created_at,updated_at) VALUES($1,$2,$3,'second','test','confirmed_hit','Second',now(),now())`, second, tenant, screening)
	if err != nil {
		t.Fatal(err)
	}
	in.EventID = uuid.New()
	in.MatchID = second
	in.Status = "confirmed_hit"
	if err := s.ReceiveScreeningReview(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetCase(adminContext(tenant), tenant, c.ID)
	if err != nil || len(got.Screenings) != 2 {
		t.Fatalf("lost match history: %+v %v", got.Screenings, err)
	}
	if count(t, db, "intake_receipts") != 2 {
		t.Fatal("missing review receipts")
	}
}
