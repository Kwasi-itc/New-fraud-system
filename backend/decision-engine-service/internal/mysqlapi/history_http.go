package mysqlapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"reflect"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (h *ExecutionHTTP) registerHistory(v *gin.RouterGroup) {
	v.GET("/tenants/:tenantId/outbox-events", h.handle("history.outbox"))
	for _, route := range []struct{ path, op string }{
		{"/decisions", "list"}, {"/decisions/count", "count"}, {"/decisions/:decisionId", "detail"},
		{"/scenarios/:scenarioId/decisions", "scenario"}, {"/scenarios/:scenarioId/test-runs", "runs"},
		{"/test-runs/:testRunId", "run"}, {"/scenarios/:scenarioId/rule-snoozes", "snoozes"},
		{"/decisions/:decisionId/workflow-executions", "workflows"}, {"/decisions/:decisionId/scoring-requests", "requests"},
		{"/scoring-requests/:requestId", "request"},
	} {
		v.GET("/tenants/:tenantId"+route.path, h.handle("history."+route.op))
	}
}

// Cursors bind the complete filter and tenant; they convey no authorization.
type historyCursor struct {
	Version   int                      `json:"version"`
	Tenant    string                   `json:"tenant"`
	Filter    ports.DecisionListFilter `json:"filter"`
	CreatedAt time.Time                `json:"created_at"`
	ID        string                   `json:"id"`
}
type historyQuery struct {
	filter ports.DecisionListFilter
	limit  int
	after  *ports.DecisionListCursor
	total  bool
}

func parseHistoryQuery(q url.Values, op, tenant, scenario string, budget int) (historyQuery, error) {
	f := historyQuery{limit: min(50, budget)}
	if op == "outbox" {
		for k, v := range q {
			if k != "limit" || len(v) != 1 {
				return f, mysql.ErrExecutionPage
			}
			n, e := strconv.Atoi(v[0])
			if e != nil || n < 1 || n > min(500, budget) {
				return f, mysql.ErrExecutionPage
			}
			f.limit = n
		}
		return f, nil
	}
	bad := func() (historyQuery, error) { return f, mysql.ErrExecutionPage }
	list := op == "list" || op == "scenario"
	for k, v := range q {
		if len(v) != 1 {
			return bad()
		}
		if !utf8.ValidString(v[0]) {
			return bad()
		}
		if op == "snoozes" {
			if k != "object_type" && k != "object_id" {
				return bad()
			}
			continue
		}
		if !list && op != "count" {
			return bad()
		}
		value := v[0]
		switch k {
		case "limit":
			n, e := strconv.Atoi(value)
			if !list || e != nil || n < 1 || n > min(500, budget) {
				return bad()
			}
			f.limit = n
		case "cursor":
			if !list || value == "" || len(value) > 3000 {
				return bad()
			}
		case "include_total_count":
			if !list || (value != "true" && value != "false") {
				return bad()
			}
			f.total = value == "true"
		case "decision_id", "scenario_id":
			id, e := uuid.Parse(value)
			if e != nil || id == uuid.Nil {
				return bad()
			}
			if k == "decision_id" {
				f.filter.DecisionID = id.String()
			} else {
				f.filter.ScenarioID = id.String()
			}
		case "object_type":
			f.filter.ObjectType = value
		case "object_id":
			f.filter.ObjectID = value
		case "object_id_prefix":
			f.filter.ObjectIDPrefix = value
		case "outcome":
			f.filter.Outcome = value
		case "search":
			f.filter.Search = value
		case "min_score", "max_score":
			n, e := strconv.ParseInt(value, 10, 32)
			if e != nil {
				return bad()
			}
			i := int(n)
			if k == "min_score" {
				f.filter.MinScore = &i
			} else {
				f.filter.MaxScore = &i
			}
		default:
			return bad()
		}
	}
	if scenario != "" {
		if f.filter.ScenarioID != "" && f.filter.ScenarioID != scenario {
			return bad()
		}
		f.filter.ScenarioID = scenario
	}
	if f.filter.MinScore != nil && f.filter.MaxScore != nil && *f.filter.MinScore > *f.filter.MaxScore {
		return bad()
	}
	// Keep generated cursors usable within the bounded request URL even when
	// clients repeat the complete filter on the next request.
	encodedFilter, err := json.Marshal(f.filter)
	if err != nil || len(encodedFilter) > 768 {
		return bad()
	}
	if value := q.Get("cursor"); value != "" {
		raw, e := base64.RawURLEncoding.Strict().DecodeString(value)
		if e != nil {
			return bad()
		}
		var c historyCursor
		if decodeExecutionHTTP(bytes.NewReader(raw), 3000, &c) != nil {
			return bad()
		}
		canonical, e := json.Marshal(c)
		id, idErr := uuid.Parse(c.ID)
		if e != nil || !bytes.Equal(canonical, raw) || c.Version != 1 || c.Tenant != tenant || !reflect.DeepEqual(c.Filter, f.filter) || idErr != nil || id == uuid.Nil || c.CreatedAt.Year() < 1000 || c.CreatedAt.Year() > 9999 || c.CreatedAt.Nanosecond()%1000 != 0 {
			return bad()
		}
		f.after = &ports.DecisionListCursor{ID: c.ID, CreatedAt: c.CreatedAt}
	}
	return f, nil
}

func adaptHistory[A, B any](items []A, adapt func(A) B) []B {
	out := make([]B, 0, len(items))
	for _, v := range items {
		out = append(out, adapt(v))
	}
	return out
}

func (h *ExecutionHTTP) history(c *gin.Context, s *ExecutionEndpointScope, op string, q url.Values, budget int) (any, error) {
	if s.History == nil || s.Clock == nil {
		return nil, mysql.ErrDecisionAuthorizationUnavailable
	}
	tenant := uuid.MustParse(c.Param("tenantId")).String()
	parent := c.Param("scenarioId")
	if parent != "" {
		parent = uuid.MustParse(parent).String()
	}
	f, err := parseHistoryQuery(q, op, tenant, parent, budget)
	if err != nil {
		return nil, err
	}
	ctx := c.Request.Context()
	var out any
	err = s.History.ReadHistory(ctx, func(r mysql.HistoryRepositories) error {
		if parent != "" {
			if _, e := r.Scenarios.GetByID(ctx, tenant, parent); e != nil {
				return e
			}
		}
		decisionID := c.Param("decisionId")
		// Existence and dependent rows share the same snapshot and tenant scope.
		if decisionID != "" && op != "detail" {
			if e := r.Decisions.RequireHistoryDecision(ctx, tenant, decisionID); e != nil {
				return e
			}
		}
		switch op {
		case "outbox":
			items, e := r.OutboxEvents.ListByTenant(ctx, tenant, f.limit)
			if e != nil {
				return e
			}
			out = gin.H{"outbox_events": adaptHistory(items, dto.AdaptOutboxEvent)}
		case "list", "scenario":
			items, more, e := r.Decisions.ListFilteredCursor(ctx, tenant, f.filter, f.limit, f.after)
			if e != nil {
				return e
			}
			page := dto.PaginationResponse{Limit: f.limit, HasMore: more}
			if more {
				last := items[len(items)-1]
				raw, e := json.Marshal(historyCursor{1, tenant, f.filter, last.CreatedAt, last.ID})
				if e != nil {
					return e
				}
				next := base64.RawURLEncoding.EncodeToString(raw)
				page.NextCursor = &next
			}
			if f.total {
				n, e := r.Decisions.CountFiltered(ctx, tenant, f.filter)
				if e != nil {
					return e
				}
				page.TotalCount = &n
			}
			out = dto.DecisionListEnvelope{Decisions: adaptHistory(items, dto.AdaptDecision), Pagination: page}
		case "count":
			n, e := r.Decisions.CountFiltered(ctx, tenant, f.filter)
			if e != nil {
				return e
			}
			out = gin.H{"count": n}
		case "detail":
			item, e := r.Decisions.GetByID(ctx, tenant, decisionID)
			if e != nil {
				return e
			}
			rules, e := r.RuleExecutions.ListByDecision(ctx, tenant, decisionID)
			if e != nil {
				return e
			}
			out = gin.H{"decision": dto.AdaptDecisionDetail(item), "rule_executions": adaptHistory(rules, dto.AdaptRuleExecution)}
		case "runs":
			items, e := r.TestRuns.ListByScenario(ctx, tenant, parent)
			if e != nil {
				return e
			}
			out = gin.H{"test_runs": adaptHistory(items, dto.AdaptTestRun)}
		case "run":
			item, e := r.TestRuns.GetByID(ctx, tenant, c.Param("testRunId"))
			if e != nil {
				return e
			}
			out = gin.H{"test_run": dto.AdaptTestRun(item)}
		case "snoozes":
			items, e := r.RuleSnoozes.ListActive(ctx, tenant, parent, q.Get("object_type"), q.Get("object_id"), s.Clock.Now())
			if e != nil {
				return e
			}
			out = gin.H{"rule_snoozes": adaptHistory(items, dto.AdaptRuleSnooze)}
		case "workflows":
			items, e := r.WorkflowExecutions.ListByDecision(ctx, tenant, decisionID)
			if e != nil {
				return e
			}
			out = gin.H{"workflow_executions": adaptHistory(items, dto.AdaptWorkflowExecution)}
		case "requests":
			items, e := r.ScoringRequests.ListByDecision(ctx, tenant, decisionID)
			if e != nil {
				return e
			}
			out = gin.H{"scoring_requests": adaptHistory(items, dto.AdaptScoringRequest)}
		case "request":
			item, e := r.ScoringRequests.GetByID(ctx, tenant, c.Param("requestId"))
			if e != nil {
				return e
			}
			out = gin.H{"scoring_request": dto.AdaptScoringRequest(item)}
		default:
			return mysql.ErrExecutionPage
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
