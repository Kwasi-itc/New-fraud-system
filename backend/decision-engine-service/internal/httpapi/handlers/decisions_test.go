package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
)

type decisionHandlerRepoStub struct {
	items         []decision.Decision
	itemByID      decision.Decision
	hasMore       bool
	totalCount    int
	countFiltered int
	cursorItems   []decision.Decision
	cursorHasMore bool
	lastCursor    *ports.DecisionListCursor
}

func (s *decisionHandlerRepoStub) Create(context.Context, decision.Decision) (decision.Decision, error) {
	return decision.Decision{}, nil
}

func (s *decisionHandlerRepoStub) GetByID(context.Context, string, string) (decision.Decision, error) {
	return s.itemByID, nil
}

func (s *decisionHandlerRepoStub) ListByTenant(context.Context, string) ([]decision.Decision, error) {
	return s.items, nil
}

func (s *decisionHandlerRepoStub) ListByTenantPage(context.Context, string, int, int) ([]decision.Decision, bool, error) {
	return s.items, s.hasMore, nil
}

func (s *decisionHandlerRepoStub) CountByTenant(context.Context, string) (int, error) {
	return s.totalCount, nil
}

func (s *decisionHandlerRepoStub) ListByScenario(context.Context, string, string) ([]decision.Decision, error) {
	return s.items, nil
}

func (s *decisionHandlerRepoStub) ListByScenarioPage(context.Context, string, string, int, int) ([]decision.Decision, bool, error) {
	return s.items, s.hasMore, nil
}

func (s *decisionHandlerRepoStub) CountByScenario(context.Context, string, string) (int, error) {
	return s.totalCount, nil
}

func (s *decisionHandlerRepoStub) ListByObject(context.Context, string, string, string) ([]decision.Decision, error) {
	return s.items, nil
}

func (s *decisionHandlerRepoStub) ListByObjectPage(context.Context, string, string, string, int, int) ([]decision.Decision, bool, error) {
	return s.items, s.hasMore, nil
}

func (s *decisionHandlerRepoStub) CountByObject(context.Context, string, string, string) (int, error) {
	return s.totalCount, nil
}

func (s *decisionHandlerRepoStub) ListFiltered(context.Context, string, ports.DecisionListFilter) ([]decision.Decision, error) {
	return s.items, nil
}

func (s *decisionHandlerRepoStub) ListFilteredPage(context.Context, string, ports.DecisionListFilter, int, int) ([]decision.Decision, bool, error) {
	return s.items, s.hasMore, nil
}

func (s *decisionHandlerRepoStub) ListFilteredCursor(_ context.Context, _ string, _ ports.DecisionListFilter, _ int, cursor *ports.DecisionListCursor) ([]decision.Decision, bool, error) {
	s.lastCursor = cursor
	return s.cursorItems, s.cursorHasMore, nil
}

func (s *decisionHandlerRepoStub) CountFiltered(context.Context, string, ports.DecisionListFilter) (int, error) {
	s.countFiltered++
	return s.totalCount, nil
}

type decisionHandlerRuleExecutionRepoStub struct {
	items []decision.RuleExecution
}

func (s decisionHandlerRuleExecutionRepoStub) CreateMany(context.Context, []decision.RuleExecution) ([]decision.RuleExecution, error) {
	return nil, nil
}

func (s decisionHandlerRuleExecutionRepoStub) ListByDecision(context.Context, string, string) ([]decision.RuleExecution, error) {
	return s.items, nil
}

func newDecisionHandlerForTests(repo ports.DecisionRepository) DecisionHandler {
	return newDecisionHandlerWithRuleRepoForTests(repo, nil)
}

func newDecisionHandlerWithRuleRepoForTests(repo ports.DecisionRepository, ruleExecRepo ports.RuleExecutionRepository) DecisionHandler {
	decisionService := service.NewDecisionService(
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		repo,
		ruleExecRepo,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		"",
		nil,
		0,
		0,
		nil,
	)
	return NewDecisionHandler(decisionService, service.ExecutionService{}, 0, false)
}

func TestDecisionHandlerListDecisionsOmitsTotalCountByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := &decisionHandlerRepoStub{
		items: []decision.Decision{{
			ID:                  "dec-1",
			TenantID:            "tenant-1",
			ScenarioID:          "scenario-1",
			ScenarioIterationID: "iter-1",
			ObjectID:            "obj-1",
			ObjectType:          "transactions",
			Outcome:             decision.OutcomeApprove,
			Score:               12,
			Triggered:           true,
			CreatedAt:           time.Now().UTC(),
		}},
		hasMore:    true,
		totalCount: 101,
	}
	handler := newDecisionHandlerForTests(repo)
	router := gin.New()
	router.GET("/v1/tenants/:tenantId/decisions", handler.ListDecisions)

	req := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-1/decisions?limit=1&offset=0", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	pagination, ok := payload["pagination"].(map[string]any)
	if !ok {
		t.Fatalf("pagination missing or invalid: %#v", payload["pagination"])
	}
	if _, exists := pagination["total_count"]; exists {
		t.Fatalf("total_count present by default, want omitted: %#v", pagination)
	}
	if repo.countFiltered != 0 {
		t.Fatalf("countFiltered = %d, want 0", repo.countFiltered)
	}
}

func TestDecisionHandlerListDecisionsIncludesTotalCountWhenRequested(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := &decisionHandlerRepoStub{
		items: []decision.Decision{{
			ID:                  "dec-1",
			TenantID:            "tenant-1",
			ScenarioID:          "scenario-1",
			ScenarioIterationID: "iter-1",
			ObjectID:            "obj-1",
			ObjectType:          "transactions",
			Outcome:             decision.OutcomeApprove,
			Score:               12,
			Triggered:           true,
			CreatedAt:           time.Now().UTC(),
		}},
		hasMore:    true,
		totalCount: 101,
	}
	handler := newDecisionHandlerForTests(repo)
	router := gin.New()
	router.GET("/v1/tenants/:tenantId/decisions", handler.ListDecisions)

	req := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-1/decisions?limit=1&offset=0&include_total_count=true", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	pagination, ok := payload["pagination"].(map[string]any)
	if !ok {
		t.Fatalf("pagination missing or invalid: %#v", payload["pagination"])
	}
	if got, ok := pagination["total_count"].(float64); !ok || int(got) != 101 {
		t.Fatalf("total_count = %#v, want 101", pagination["total_count"])
	}
	if repo.countFiltered != 1 {
		t.Fatalf("countFiltered = %d, want 1", repo.countFiltered)
	}
}

func TestDecisionHandlerListDecisionsSupportsCursorPagination(t *testing.T) {
	gin.SetMode(gin.TestMode)

	firstPageCreatedAt := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	repo := &decisionHandlerRepoStub{
		cursorItems: []decision.Decision{
			{
				ID:                  "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
				TenantID:            "tenant-1",
				ScenarioID:          "scenario-1",
				ScenarioIterationID: "iter-1",
				ObjectID:            "obj-1",
				ObjectType:          "transactions",
				Outcome:             decision.OutcomeApprove,
				Score:               12,
				Triggered:           true,
				CreatedAt:           firstPageCreatedAt,
			},
			{
				ID:                  "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
				TenantID:            "tenant-1",
				ScenarioID:          "scenario-1",
				ScenarioIterationID: "iter-1",
				ObjectID:            "obj-2",
				ObjectType:          "transactions",
				Outcome:             decision.OutcomeReview,
				Score:               18,
				Triggered:           true,
				CreatedAt:           firstPageCreatedAt.Add(-time.Minute),
			},
		},
		cursorHasMore: true,
	}
	handler := newDecisionHandlerForTests(repo)
	router := gin.New()
	router.GET("/v1/tenants/:tenantId/decisions", handler.ListDecisions)

	cursor := service.EncodeDecisionCursor(firstPageCreatedAt.Add(time.Minute), "cccccccc-cccc-cccc-cccc-cccccccccccc")
	req := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-1/decisions?cursor="+cursor+"&limit=2", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	pagination, ok := payload["pagination"].(map[string]any)
	if !ok {
		t.Fatalf("pagination missing or invalid: %#v", payload["pagination"])
	}
	if _, exists := pagination["next_offset"]; exists {
		t.Fatalf("next_offset present for cursor pagination, want omitted: %#v", pagination)
	}
	nextCursor, ok := pagination["next_cursor"].(string)
	if !ok || nextCursor == "" {
		t.Fatalf("next_cursor = %#v, want non-empty string", pagination["next_cursor"])
	}
	if repo.lastCursor == nil || repo.lastCursor.ID != "cccccccc-cccc-cccc-cccc-cccccccccccc" {
		t.Fatalf("lastCursor = %+v, want decoded request cursor", repo.lastCursor)
	}

	decoded, err := service.DecodeDecisionCursor(nextCursor)
	if err != nil {
		t.Fatalf("DecodeDecisionCursor(next_cursor) error = %v", err)
	}
	if decoded.ID != "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb" {
		t.Fatalf("decoded next cursor id = %q, want last item id", decoded.ID)
	}
}

func TestDecisionHandlerGetDecisionIncludesRuleEvaluationEvidence(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := &decisionHandlerRepoStub{
		itemByID: decision.Decision{
			ID:                  "dec-1",
			TenantID:            "tenant-1",
			ScenarioID:          "scenario-1",
			ScenarioIterationID: "iter-1",
			ObjectID:            "obj-1",
			ObjectType:          "transactions",
			Outcome:             decision.OutcomeReview,
			Score:               25,
			Triggered:           true,
			CreatedAt:           time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC),
		},
	}
	ruleExecRepo := decisionHandlerRuleExecutionRepoStub{
		items: []decision.RuleExecution{{
			ID:            "rule-exec-1",
			DecisionID:    "dec-1",
			RuleID:        "rule-1",
			RuleName:      "amount threshold",
			Outcome:       "hit",
			ScoreModifier: 25,
			Evaluation: &domainast.EvaluationNode{
				Function:    "gt",
				ReturnValue: true,
				Children: []domainast.EvaluationNode{
					{Function: "Payload", ReturnValue: 500.0},
					{Function: "constant", Constant: 300.0, ReturnValue: 300.0},
				},
			},
			CreatedAt: time.Date(2026, 7, 28, 10, 0, 1, 0, time.UTC),
		}},
	}

	handler := newDecisionHandlerWithRuleRepoForTests(repo, ruleExecRepo)
	router := gin.New()
	router.GET("/v1/tenants/:tenantId/decisions/:decisionId", handler.GetDecision)

	req := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-1/decisions/dec-1", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload struct {
		RuleExecutions []struct {
			Evaluation struct {
				Function    string `json:"function"`
				ReturnValue any    `json:"return_value"`
				Children    []struct {
					ReturnValue any `json:"return_value"`
				} `json:"children"`
			} `json:"evaluation"`
		} `json:"rule_executions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(payload.RuleExecutions) != 1 {
		t.Fatalf("len(rule_executions) = %d, want 1", len(payload.RuleExecutions))
	}
	evaluation := payload.RuleExecutions[0].Evaluation
	if evaluation.Function != "gt" {
		t.Fatalf("evaluation.function = %q, want gt", evaluation.Function)
	}
	if evaluation.ReturnValue != true {
		t.Fatalf("evaluation.return_value = %#v, want true", evaluation.ReturnValue)
	}
	if len(evaluation.Children) != 2 {
		t.Fatalf("len(evaluation.children) = %d, want 2", len(evaluation.Children))
	}
}
