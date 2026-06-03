package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
)

func TestBuildScreeningDispatchRequestUsesQueryFieldAndConfig(t *testing.T) {
	t.Parallel()

	cfg := screening.Config{
		ID:       "cfg-1",
		Provider: "opensanctions",
		ConfigJSON: json.RawMessage(`{
			"entity_type":"Organization",
			"query_fields":{"name":"company_name"},
			"provider_config":{"datasets":["pep"]},
			"limit_override":15,
			"counterparty_id_field":"counterparty_id"
		}`),
	}
	exec := screening.Execution{
		DecisionID: "decision-1",
		ScenarioID: "scenario-1",
	}

	raw, err := buildScreeningDispatchRequest("exec-1", cfg, exec, "company", "obj-1", map[string]any{
		"company_name":    "Acme Ltd",
		"counterparty_id": "cp-1",
	})
	if err != nil {
		t.Fatalf("buildScreeningDispatchRequest() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if got["idempotency_key"] != "exec-1" {
		t.Fatalf("idempotency_key = %v, want exec-1", got["idempotency_key"])
	}
	queries, ok := got["queries"].([]any)
	if !ok || len(queries) != 1 {
		t.Fatalf("queries = %v, want one query", got["queries"])
	}
	query := queries[0].(map[string]any)
	if query["name"] != "Acme Ltd" {
		t.Fatalf("query name = %v, want Acme Ltd", query["name"])
	}
	if query["type"] != "Organization" {
		t.Fatalf("query type = %v, want Organization", query["type"])
	}
}

func TestScreeningServiceUpdateExecutionStatusFromScreeningCallback(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 29, 9, 0, 0, 0, time.UTC)
	repo := &screeningExecutionRepoForTest{
		items: []screening.Execution{{
			ID:          "exec-1",
			TenantID:    "tenant-1",
			ConfigID:    "cfg-1",
			DecisionID:  "decision-1",
			ScenarioID:  "scenario-1",
			Status:      screening.ExecutionStatusPending,
			RequestJSON: json.RawMessage(`{"idempotency_key":"exec-1"}`),
		}},
	}
	svc := NewScreeningService(
		txManagerStub{store: mutationStoreStub{screeningExecutionRepo: repo}},
		fixedIDGenerator{id: uuid.MustParse("33333333-3333-3333-3333-333333333333")},
		fixedClock{now: now},
		scenarioRepoStub{item: scenario.Scenario{ID: "scenario-1", TenantID: "tenant-1"}},
		nilScreeningConfigRepository{},
		repo,
	)

	updated, err := svc.UpdateExecutionStatusFromScreeningCallback(context.Background(), "tenant-1", ScreeningStatusUpdate{
		ScreeningID:       "scr-1",
		DecisionID:        "decision-1",
		ScenarioID:        "scenario-1",
		ScreeningConfigID: "cfg-1",
		Status:            "awaiting_review",
		Provider:          "opensanctions",
		ObjectType:        "company",
		ObjectID:          "obj-1",
		ProviderReference: "provider-1",
		MatchCount:        2,
	})
	if err != nil {
		t.Fatalf("UpdateExecutionStatusFromScreeningCallback() error = %v", err)
	}
	if updated.Status != screening.ExecutionStatusCompleted {
		t.Fatalf("status = %s, want %s", updated.Status, screening.ExecutionStatusCompleted)
	}
	if updated.ProviderReference != "provider-1" {
		t.Fatalf("provider reference = %q, want provider-1", updated.ProviderReference)
	}
	var response map[string]any
	if err := json.Unmarshal(updated.ResponseJSON, &response); err != nil {
		t.Fatalf("unmarshal response json: %v", err)
	}
	if response["screening_id"] != "scr-1" {
		t.Fatalf("screening_id = %v, want scr-1", response["screening_id"])
	}
}

type screeningExecutionRepoForTest struct {
	items []screening.Execution
}

func (r *screeningExecutionRepoForTest) CreateMany(context.Context, []screening.Execution) ([]screening.Execution, error) {
	return nil, nil
}

func (r *screeningExecutionRepoForTest) GetByID(_ context.Context, tenantID, executionID string) (screening.Execution, error) {
	for _, item := range r.items {
		if item.TenantID == tenantID && item.ID == executionID {
			return item, nil
		}
	}
	return screening.Execution{}, errors.New("not found")
}

func (r *screeningExecutionRepoForTest) ListByDecision(_ context.Context, tenantID, decisionID string) ([]screening.Execution, error) {
	var out []screening.Execution
	for _, item := range r.items {
		if item.TenantID == tenantID && item.DecisionID == decisionID {
			out = append(out, item)
		}
	}
	return out, nil
}

func (r *screeningExecutionRepoForTest) ListByStatus(context.Context, screening.ExecutionStatus, int) ([]screening.Execution, error) {
	return nil, nil
}

func (r *screeningExecutionRepoForTest) Update(_ context.Context, item screening.Execution) (screening.Execution, error) {
	for i := range r.items {
		if r.items[i].ID == item.ID {
			r.items[i] = item
		}
	}
	return item, nil
}

func (r *screeningExecutionRepoForTest) UpdateStatus(context.Context, string, screening.ExecutionStatus) error {
	return nil
}
