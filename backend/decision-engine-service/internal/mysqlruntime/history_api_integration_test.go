package mysqlruntime

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/snooze"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/google/uuid"
)

func testHistoryAPIs(t *testing.T, h http.Handler, base string, db *sql.DB, database string, revoked *atomic.Bool) {
	ctx := context.Background()
	tenant := uuid.MustParse(strings.TrimPrefix(base, "/v1/tenants/"))
	parent, other := uuid.NewString(), uuid.NewString()
	live, phantom, rule := uuid.NewString(), uuid.NewString(), uuid.NewString()
	at := fixtureClock{}.Now()
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	// Fixture-only seed authority, never installed in the HTTP application.
	auth := func(context.Context, *sql.Tx, uuid.UUID, mysql.AuthoringAction) error { return nil }
	readAuth := func(context.Context, *sql.Tx, uuid.UUID, bool) error { return nil }
	a, e := mysql.NewAuthoringStore(db, database, tenant, 500, 2<<20, auth)
	must(e)
	must(a.Mutate(ctx, func(r mysql.AuthoringRepositories) error {
		for i, id := range []string{parent, other} {
			if _, e := r.Scenarios.Create(ctx, scenario.Scenario{ID: id, TenantID: tenant.String(), Name: []string{"History fixture", "Empty history"}[i], TriggerObjectType: "wallets", CreatedAt: at, UpdatedAt: at}); e != nil {
				return e
			}
		}
		for _, id := range []string{live, phantom} {
			v, e := r.Iterations.NextVersion(ctx, tenant.String(), parent)
			if e != nil {
				return e
			}
			if _, e = r.Iterations.Create(ctx, scenario.Iteration{ID: id, TenantID: tenant.String(), ScenarioID: parent, Version: v, Status: scenario.IterationStatusDraft, TriggerFormula: []byte(`{"constant":true}`), CreatedAt: at}); e != nil {
				return e
			}
		}
		_, e := r.Rules.Create(ctx, scenario.Rule{ID: rule, TenantID: tenant.String(), IterationID: live, Name: "History rule", Formula: []byte(`{"constant":true}`), CreatedAt: at, UpdatedAt: at})
		return e
	}))
	store, e := mysql.NewDecisionStore(db, database, tenant, 500, 2<<20, readAuth)
	must(e)
	ids := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	run := uuid.NewString()
	snoozeID := uuid.NewString()
	must(store.Mutate(ctx, func(r mysql.DecisionRepositories) error {
		for i, id := range ids {
			_, e := r.Decisions.Create(ctx, decision.Decision{ID: id, TenantID: tenant.String(), ScenarioID: parent, ScenarioIterationID: live, ObjectID: "Wallet ", ObjectType: "wallets", Outcome: decision.OutcomeReview, Score: i, Triggered: true, RequestBody: []byte(`{"n":9007199254740993}`), CreatedAt: at})
			if e != nil {
				return e
			}
		}
		if _, e := r.RuleExecutions.CreateMany(ctx, []decision.RuleExecution{{ID: uuid.NewString(), DecisionID: ids[0], RuleID: rule, RuleName: "History rule", Outcome: "match", ScoreModifier: 4, CreatedAt: at}}); e != nil {
			return e
		}
		if _, e := r.TestRuns.Create(ctx, scenario.TestRun{ID: run, TenantID: tenant.String(), ScenarioID: parent, LiveIterationID: live, PhantomIterationID: phantom, Status: scenario.TestRunStatusPending, CreatedAt: at, UpdatedAt: at, ExpiresAt: at.Add(time.Hour)}); e != nil {
			return e
		}
		for i, expiry := range []time.Time{at.Add(time.Hour), at.Add(-time.Hour)} {
			id := snoozeID
			if i > 0 {
				id = uuid.NewString()
			}
			if _, e := r.RuleSnoozes.Create(ctx, snooze.RuleSnooze{ID: id, TenantID: tenant.String(), ScenarioID: parent, SnoozeGroupID: uuid.NewString(), ObjectType: "wallets", ObjectID: "Wallet ", CreatedAt: at.Add(-2 * time.Hour), ExpiresAt: expiry}); e != nil {
				return e
			}
		}
		return nil
	}))
	config, wid, requestID, executionID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	cfg, e := mysql.NewConfigurationStore(db, database, tenant, 500, 2<<20, auth)
	must(e)
	must(cfg.Mutate(ctx, func(r mysql.ConfigurationRepositories) error {
		if _, e := r.ScoringConfigs.Create(ctx, scoring.Config{ID: config, TenantID: tenant.String(), ScenarioID: parent, Name: "history", RulesetRef: "fixture", ConfigJSON: []byte(`{}`), AllowedOutcomes: []string{"review"}, CreatedAt: at, UpdatedAt: at}); e != nil {
			return e
		}
		_, e := r.Workflows.Create(ctx, workflow.Definition{ID: wid, TenantID: tenant.String(), ScenarioID: parent, Name: "history", ActionType: workflow.ActionTypeEmitEvent, ActionConfig: []byte(`{"url":"https://example.test/event"}`), AllowedOutcomes: []string{"review"}, CreatedAt: at, UpdatedAt: at})
		return e
	}))
	effects, e := mysql.NewEffectStore(db, database, tenant, 500, 2<<20, readAuth, nil)
	must(e)
	must(effects.Mutate(ctx, func(r mysql.EffectRepositories) error {
		if _, e := r.ScoringRequests.CreateMany(ctx, []scoring.Request{{ID: requestID, TenantID: tenant.String(), ConfigID: config, DecisionID: ids[0], ScenarioID: parent, Status: scoring.RequestStatusPending, RequestJSON: []byte(`{"n":9007199254740993}`), CreatedAt: at, UpdatedAt: at}}); e != nil {
			return e
		}
		_, e := r.WorkflowExecutions.CreateMany(ctx, []workflow.Execution{{ID: executionID, TenantID: tenant.String(), WorkflowID: &wid, DecisionID: ids[0], ScenarioID: parent, ActionType: workflow.ActionTypeEmitEvent, Status: workflow.ExecutionStatusPendingDispatch, ActionConfig: []byte(`{"url":"https://example.test/event"}`), CreatedAt: at}})
		return e
	}))
	get := func(t *testing.T, path string, auth bool, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("GET", path, nil)
		if auth {
			req.Header.Set("Authorization", "Bearer fixture-only")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("%s: got %d want %d: %s", path, rec.Code, want, rec.Body.String())
		}
		return rec
	}
	cases := []struct {
		name, path, key, id string
		collection          bool
	}{
		{"T11 decision list", "/decisions?scenario_id=" + parent, "decisions", ids[0], true},
		{"T12 scenario decisions", "/scenarios/" + parent + "/decisions", "decisions", ids[0], true},
		{"T13 decision count", "/decisions/count?scenario_id=" + parent, "count", "", false},
		{"T14 decision evidence", "/decisions/" + ids[0], "decision", ids[0], false},
		{"T15 test run list", "/scenarios/" + parent + "/test-runs", "test_runs", run, true},
		{"T16 test run detail", "/test-runs/" + run, "test_run", run, false},
		{"T17 active snoozes", "/scenarios/" + parent + "/rule-snoozes?object_type=wallets&object_id=Wallet%20", "rule_snoozes", snoozeID, true},
		{"T18 workflow history", "/decisions/" + ids[0] + "/workflow-executions", "workflow_executions", executionID, true},
		{"T19 scoring history", "/decisions/" + ids[0] + "/scoring-requests", "scoring_requests", requestID, true},
		{"T20 scoring detail", "/scoring-requests/" + requestID, "scoring_request", requestID, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			get(t, base+tc.path, false, 401)
			get(t, "/v1/tenants/"+uuid.NewString()+tc.path, true, 403)
			revoked.Store(true)
			get(t, base+tc.path, true, 403)
			revoked.Store(false)
			rec := get(t, base+tc.path, true, 200)
			var out map[string]json.RawMessage
			must(json.Unmarshal(rec.Body.Bytes(), &out))
			if tc.key == "count" {
				if string(out[tc.key]) != "3" {
					t.Fatal(rec.Body.String())
				}
			} else if !strings.Contains(string(out[tc.key]), tc.id) {
				t.Fatal(rec.Body.String())
			}
			sep := "?"
			if strings.Contains(tc.path, "?") {
				sep = "&"
			}
			get(t, base+tc.path+sep+"ignored=1", true, 400)
			if tc.name == "T14 decision evidence" {
				if !strings.Contains(string(out["rule_executions"]), rule) || !strings.Contains(rec.Body.String(), "9007199254740993") {
					t.Fatal(rec.Body.String())
				}
			}
			if tc.collection {
				var items []json.RawMessage
				must(json.Unmarshal(out[tc.key], &items))
				want := 1
				if tc.key == "decisions" {
					want = 3
					if strings.Contains(string(out[tc.key]), "request_body") {
						t.Fatal("list exposed payload")
					}
				}
				if len(items) != want {
					t.Fatal(rec.Body.String())
				}
			}
		})
	}
	t.Run("history keyset ties filters and missing parents", func(t *testing.T) {
		path := base + "/scenarios/" + parent + "/decisions?limit=1&include_total_count=true"
		seen := map[string]bool{}
		for i := 0; i < 3; i++ {
			rec := get(t, path, true, 200)
			var out dto.DecisionListEnvelope
			must(json.Unmarshal(rec.Body.Bytes(), &out))
			if len(out.Decisions) != 1 || seen[out.Decisions[0].ID] || out.Pagination.TotalCount == nil || *out.Pagination.TotalCount != 3 {
				t.Fatal(rec.Body.String())
			}
			seen[out.Decisions[0].ID] = true
			if i < 2 {
				if out.Pagination.NextCursor == nil {
					t.Fatal("missing cursor")
				}
				cursor := url.QueryEscape(*out.Pagination.NextCursor)
				get(t, base+"/scenarios/"+other+"/decisions?cursor="+cursor, true, 400)
				get(t, base+"/scenarios/"+parent+"/decisions?min_score=1&cursor="+cursor, true, 400)
				path = base + "/scenarios/" + parent + "/decisions?limit=1&include_total_count=true&cursor=" + cursor
			} else if out.Pagination.HasMore || out.Pagination.NextCursor != nil {
				t.Fatal("false continuation")
			}
		}
		rec := get(t, base+"/decisions/count?scenario_id="+parent+"&min_score=1&max_score=1&object_id=Wallet%20", true, 200)
		if !strings.Contains(rec.Body.String(), `"count":1`) {
			t.Fatal(rec.Body.String())
		}
		rec = get(t, base+"/scenarios/"+parent+"/rule-snoozes?object_type=wallets&object_id=Wallet", true, 200)
		if !strings.Contains(rec.Body.String(), `"rule_snoozes":[]`) {
			t.Fatal("trailing-space lookup", rec.Body.String())
		}
		for _, path := range []string{"/decisions/" + uuid.NewString(), "/decisions/" + uuid.NewString() + "/workflow-executions", "/decisions/" + uuid.NewString() + "/scoring-requests", "/test-runs/" + uuid.NewString(), "/scoring-requests/" + uuid.NewString(), "/scenarios/" + uuid.NewString() + "/test-runs"} {
			get(t, base+path, true, 404)
		}
		for _, q := range []string{"limit=0", "limit=501", "limit=1&limit=2", "cursor=invalid", "min_score=2&max_score=1", "offset=1000000", "scenario_id=invalid"} {
			get(t, base+"/decisions?"+q, true, 400)
		}
	})
}
