package mysqlapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/testsupport"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Isolated adapter fixture only, not a release migration/grants rehearsal. No
// application DSN is accepted. Raw candidate DDL is installed in a GENERATED DB
// on the pinned private Compose server, then that exact DB is dropped.
func executionAPIFixture(t *testing.T) (*sql.DB, string) { return testsupport.DecisionMySQLDatabase(t) }

func TestMySQLExecutionReadCancellationAdapterAuthorizationAndRollback(t *testing.T) {
	db, database := executionAPIFixture(t)
	ctx := context.Background()
	tenant, parent := uuid.New(), uuid.New()
	a, err := mysql.NewAuthoringStore(db, database, tenant, 500, 2<<20, func(context.Context, *sql.Tx, uuid.UUID, mysql.AuthoringAction) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	iteration := scenario.Iteration{ID: uuid.NewString(), TenantID: tenant.String(), ScenarioID: parent.String(), Status: scenario.IterationStatusDraft, TriggerFormula: []byte(`{"constant":true}`), CreatedAt: at}
	if err := a.Mutate(ctx, func(r mysql.AuthoringRepositories) error {
		if _, err := r.Scenarios.Create(ctx, scenario.Scenario{ID: parent.String(), TenantID: tenant.String(), Name: "Adapter fixture", Description: "test", TriggerObjectType: "wallets", CreatedAt: at, UpdatedAt: at}); err != nil {
			return err
		}
		version, err := r.Iterations.NextVersion(ctx, tenant.String(), parent.String())
		if err != nil {
			return err
		}
		iteration.Version = version
		if _, err := r.Iterations.Create(ctx, iteration); err != nil {
			return err
		}
		_, err = r.Iterations.Commit(ctx, tenant.String(), parent.String(), iteration.ID, at)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var revoked atomic.Bool
	queues := map[ports.JobKind]mysql.DeliveryQueuePolicy{}
	for _, kind := range []ports.JobKind{ports.JobScheduled, ports.JobAsync, ports.JobCallback} {
		queues[kind] = mysql.DeliveryQueuePolicy{Name: string(kind), MaxAttempts: 3}
	}
	s, err := mysql.NewExecutionStore(db, database, tenant, 500, 2<<20, func(_ context.Context, tx *sql.Tx, id uuid.UUID, action mysql.ExecutionAction) error {
		if revoked.Load() || tx == nil || id != tenant {
			return mysql.ErrDecisionAuthorizationDenied
		}
		return nil // Fixture authority only.
	}, mysql.ExecutionPolicy{Queues: queues, CallbackMaxAttempts: 3})
	if err != nil {
		t.Fatal(err)
	}
	scheduled := execution.ScheduledExecution{ID: uuid.NewString(), TenantID: tenant.String(), ScenarioID: parent.String(), ScenarioIterationID: iteration.ID, Source: execution.SourceManual, Status: execution.StatusPending, MaxAttempts: 3, ScheduledFor: at, CreatedAt: at, RequestBody: []byte(`{"items":[]}`)}
	async := execution.AsyncDecisionExecution{ID: uuid.NewString(), TenantID: tenant.String(), ScenarioID: parent.String(), ObjectType: "wallets", Status: execution.StatusQueued, MaxAttempts: 3, CallbackURL: "https://callback.invalid", CreatedAt: at, RequestBody: []byte(`{"items":[]}`)}
	if err := s.Run(ctx, mysql.ExecutionRequest, func(r mysql.ExecutionRepositories) error {
		if _, err := r.Scheduled.Create(ctx, scheduled); err != nil {
			return err
		}
		_, err := r.Async.Create(ctx, async)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	api, err := NewExecutionAdapter(s)
	if err != nil {
		t.Fatal(err)
	}
	zero, one := 0, 1
	gin.SetMode(gin.TestMode)
	httpAPI, err := NewExecutionHTTP(func(context.Context, uuid.UUID) (*ExecutionEndpointScope, error) {
		return &ExecutionEndpointScope{Read: api}, nil
	}, 4096)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	httpAPI.Register(router.Group("/v1"))
	request := func(method, path, body string, want int) {
		t.Helper()
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		if rec.Code != want {
			t.Fatal(method, path, rec.Code, rec.Body.String())
		}
	}
	base := "/v1/tenants/" + tenant.String() + "/async-decision-executions"
	request("GET", base+"/"+async.ID, "", 200)
	request("GET", "/v1/tenants/"+uuid.NewString()+"/async-decision-executions/"+async.ID, "", 400)
	request("POST", base+"/"+async.ID+"/cancel", `{"expected_generation":1}`, 409)
	request("POST", base+"/"+async.ID+"/cancel", `{"expected_generation":0,"expected_generation":1}`, 400)
	request("POST", base, `{"items":[]}`, 503) // No submission/model provider configured.
	if _, err := api.GetAsync(ctx, uuid.NewString(), async.ID); !errors.Is(err, mysql.ErrTenantScope) {
		t.Fatal("foreign read", err)
	}
	if _, err := api.CancelScheduled(ctx, tenant.String(), uuid.NewString(), scheduled.ID, CancellationRequest{&zero}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("wrong scenario cancelled", err)
	}
	if _, err := api.CancelScheduled(ctx, tenant.String(), parent.String(), scheduled.ID, CancellationRequest{&one}); !errors.Is(err, mysql.ErrExecutionState) {
		t.Fatal("stale generation cancelled", err)
	}
	before, err := api.GetScheduled(ctx, tenant.String(), parent.String(), scheduled.ID)
	if err != nil || before.Status != "pending" {
		t.Fatal(before, err)
	}
	cancelled, err := api.CancelScheduled(ctx, tenant.String(), parent.String(), scheduled.ID, CancellationRequest{&zero})
	if err != nil || cancelled.Status != "cancelled" || cancelled.CancelledAt == nil || cancelled.Generation != 0 {
		t.Fatal(cancelled, err)
	}
	replayed, err := api.CancelScheduled(ctx, tenant.String(), parent.String(), scheduled.ID, CancellationRequest{&zero})
	if err != nil || !replayed.CancelledAt.Equal(*cancelled.CancelledAt) {
		t.Fatal("cancellation replay", replayed, err)
	}
	schema, _ := mysqlstore.Identifier(database)
	if _, err := db.ExecContext(ctx, "CREATE TRIGGER "+schema+".fixture_api_callback_failure BEFORE INSERT ON "+schema+".decision_engine_execution_job_deliveries FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='callback required'"); err != nil {
		t.Fatal(err)
	}
	failed, err := api.CancelAsync(ctx, tenant.String(), async.ID, CancellationRequest{&zero})
	if err == nil || failed.ID != "" {
		t.Fatal("failed mutation leaked provisional response", failed, err)
	}
	current, err := api.GetAsync(ctx, tenant.String(), async.ID)
	if err != nil || current.Status != "queued" || current.CancelledAt != nil || current.CallbackStatus != "" {
		t.Fatal("callback failure did not roll back", current, err)
	}
	if _, err := db.ExecContext(ctx, "DROP TRIGGER "+schema+".fixture_api_callback_failure"); err != nil {
		t.Fatal(err)
	}
	current, err = api.CancelAsync(ctx, tenant.String(), async.ID, CancellationRequest{&zero})
	if err != nil || current.Status != "cancelled" || current.CallbackStatus != "pending" {
		t.Fatal(current, err)
	}
	current, err = api.CancelCallback(ctx, tenant.String(), async.ID, CancellationRequest{&zero})
	if err != nil || current.Status != "cancelled" || current.CallbackStatus != "cancelled" || current.CallbackCancelledAt == nil {
		t.Fatal("callback-only cancellation", current, err)
	}
	if observed, completed, err := api.WaitAsync(ctx, tenant.String(), async.ID, 0, ExecutionWaitPolicy{MaxWindow: time.Second, PollInterval: 10 * time.Millisecond}); err != nil || completed || observed.Status != "cancelled" {
		t.Fatal("cancelled state became inline success", observed, completed, err)
	}
	redriven, err := api.RedriveCallback(ctx, tenant.String(), async.ID, RedriveRequest{&zero})
	if err != nil || redriven.Generation != 1 || redriven.Status != "cancelled" || redriven.CallbackStatus != "pending" {
		t.Fatal("callback redrive lost evaluation state", redriven, err)
	}
	if _, err := api.RedriveCallback(ctx, tenant.String(), async.ID, RedriveRequest{&zero}); !errors.Is(err, mysql.ErrExecutionState) {
		t.Fatal("stale callback generation accepted", err)
	}
	if _, err := api.RedriveScheduled(ctx, tenant.String(), uuid.NewString(), scheduled.ID, RedriveRequest{&zero}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("foreign scenario redrive", err)
	}
	if _, err := api.RedriveAsync(ctx, uuid.NewString(), async.ID, RedriveRequest{&one}); !errors.Is(err, mysql.ErrTenantScope) {
		t.Fatal("foreign async redrive", err)
	}
	// Equal timestamps force UUID tie-breaking. Large owner bodies must not
	// appear in collection projections; filters execute in SQL before LIMIT.
	if err := s.Run(ctx, mysql.ExecutionRequest, func(r mysql.ExecutionRepositories) error {
		for range 4 {
			v := async
			v.ID = uuid.NewString()
			v.CallbackURL = ""
			v.RequestBody = []byte(`{"secret":"` + strings.Repeat("x", 10000) + `"}`)
			if _, err := r.Async.Create(ctx, v); err != nil {
				return err
			}
			x := scheduled
			x.ID = uuid.NewString()
			if _, err := r.Scheduled.Create(ctx, x); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	foreignTenant := uuid.New()
	foreignStore, err := mysql.NewExecutionStore(db, database, foreignTenant, 500, 2<<20, func(context.Context, *sql.Tx, uuid.UUID, mysql.ExecutionAction) error { return nil }, mysql.ExecutionPolicy{Queues: queues, CallbackMaxAttempts: 3})
	if err != nil {
		t.Fatal(err)
	}
	foreign := async
	foreign.ID = uuid.NewString()
	foreign.TenantID = foreignTenant.String()
	foreign.ScenarioID = ""
	foreign.CreatedAt = at.Add(time.Hour)
	if err := foreignStore.Run(ctx, mysql.ExecutionRequest, func(r mysql.ExecutionRepositories) error { _, err := r.Async.Create(ctx, foreign); return err }); err != nil {
		t.Fatal(err)
	}
	transport, err := mysql.NewExecutionDeliveryStore(db, database, tenant, ports.JobAsync, queues[ports.JobAsync].Name, time.Minute, func(context.Context, *sql.Tx, uuid.UUID, ports.JobKind, mysql.EffectWorkerAction) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	lease, err := transport.Claim(ctx)
	if err != nil || lease == nil {
		t.Fatal(lease, err)
	}
	if err := s.Run(ctx, mysql.ExecutionRun, func(r mysql.ExecutionRepositories) error { _, err := r.Async.Start(ctx, *lease); return err }); err != nil {
		t.Fatal(err)
	}
	for _, parentID := range []string{"", parent.String()} {
		seen := map[string]bool{}
		f := mysql.ExecutionPageFilter{Limit: 2}
		for {
			page, err := api.Page(ctx, tenant.String(), parentID, f)
			if err != nil {
				t.Fatal(err)
			}
			key := "async_decision_executions"
			if parentID != "" {
				key = "scheduled_executions"
			}
			for _, v := range page[key].([]mysql.ExecutionSummary) {
				if v.ID == foreign.ID || v.TenantID != tenant.String() {
					t.Fatal("cross-tenant collection leak")
				}
				if seen[v.ID] {
					t.Fatal("duplicate page identity", v.ID)
				}
				seen[v.ID] = true
			}
			encoded, _ := json.Marshal(page)
			if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "request_body") {
				t.Fatal("large projection leak")
			}
			token, ok := page["next_cursor"].(string)
			if !ok {
				break
			}
			parsed, err := executionPageFilter(map[string][]string{"cursor": {token}, "limit": {"2"}})
			if err != nil {
				t.Fatal(err)
			}
			f = parsed
			changed := f
			changed.MinAttemptCount = 1
			if _, err := api.Page(ctx, tenant.String(), parentID, changed); !errors.Is(err, mysql.ErrExecutionPage) {
				t.Fatal("filter cursor not bound", err)
			}
		}
		if len(seen) != 5 {
			t.Fatal("missing page identities", parentID, len(seen))
		}
		page, err := api.Page(ctx, tenant.String(), parentID, mysql.ExecutionPageFilter{Status: "cancelled", Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		key := "async_decision_executions"
		if parentID != "" {
			key = "scheduled_executions"
		}
		if len(page[key].([]mysql.ExecutionSummary)) != 1 {
			t.Fatal("cancelled filter", page)
		}
		page, err = api.Page(ctx, tenant.String(), parentID, mysql.ExecutionPageFilter{MinAttemptCount: 1, Limit: 2})
		attempted := 0
		if parentID == "" {
			attempted = 1
		}
		if err != nil || len(page[key].([]mysql.ExecutionSummary)) != attempted {
			t.Fatal("attempt filter", page, err)
		}
		summary, err := api.StatusSummary(ctx, tenant.String(), parentID)
		if err != nil {
			t.Fatal(err)
		}
		if summary["status_summary"].(map[execution.Status]int)["cancelled"] != 1 {
			t.Fatal(summary)
		}
	}
	request("GET", base+"?limit=2", "", 200)
	request("GET", base+"?limit=0", "", 400)
	request("GET", base+"?limit=2&invalid=%ZZ", "", 400)
	request("GET", base+"?cursor="+strings.Repeat("x", 4097), "", 413)
	request("GET", base+"/status-summary", "", 200)
	request("GET", base+"/status-summary?status=queued", "", 400)
	request("PUT", "/v1/tenants/"+tenant.String()+"/scenarios/"+parent.String()+"/recurring-schedule", `{}`, 503)
	revoked.Store(true)
	request("GET", base+"?limit=2", "", 403)
	request("GET", base+"/status-summary", "", 403)
	request("GET", base+"/"+async.ID, "", 403)
	request("POST", base+"/"+async.ID+"/callback/retry", `{"expected_generation":1}`, 403)
	if _, err := api.GetAsync(ctx, tenant.String(), async.ID); !errors.Is(err, mysql.ErrDecisionAuthorizationDenied) {
		t.Fatal("revoked read allowed", err)
	}
	if _, err := api.CancelAsync(ctx, tenant.String(), async.ID, CancellationRequest{&zero}); !errors.Is(err, mysql.ErrDecisionAuthorizationDenied) {
		t.Fatal("revoked cancellation replay allowed", err)
	}
	if _, err := api.RedriveCallback(ctx, tenant.String(), async.ID, RedriveRequest{&one}); !errors.Is(err, mysql.ErrDecisionAuthorizationDenied) {
		t.Fatal("revoked redrive", err)
	}
}
