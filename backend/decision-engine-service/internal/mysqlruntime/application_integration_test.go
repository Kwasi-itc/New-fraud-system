package mysqlruntime

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqlapi"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqlexecution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/testsupport"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"github.com/google/uuid"
)

type fixtureIDs struct{}

func (fixtureIDs) New() uuid.UUID { return uuid.New() }

type fixtureClock struct{}

func (fixtureClock) Now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

type fixtureModel struct {
	tenant uuid.UUID
	model  ports.TenantModel
}

func (f *fixtureModel) GetTenantModel(_ context.Context, tenant string) (ports.TenantModel, error) {
	if tenant != f.tenant.String() {
		return ports.TenantModel{}, mysql.ErrTenantScope
	}
	return f.model, nil
}

type unusedDispatch struct{}

type fixtureIndexPreparation struct{ tenant uuid.UUID }

func (f fixtureIndexPreparation) ListIndexJobsForTables(_ context.Context, tenant string, tables []string, limit int) ([]ports.ManagedIndexJob, error) {
	if tenant != f.tenant.String() || len(tables) != 1 || limit < 1 {
		return nil, errors.New("wrong fixture index scope")
	}
	return []ports.ManagedIndexJob{}, nil
}
func (f fixtureIndexPreparation) CreateIndexJob(context.Context, string, string, string, []string, string) (ports.ManagedIndexJob, error) {
	return ports.ManagedIndexJob{}, errors.New("fixture has no index requirement")
}
func (f fixtureIndexPreparation) RetryIndexJob(context.Context, string) error {
	return errors.New("fixture has no failed index job")
}

func (unusedDispatch) Deliver(context.Context, ports.DispatchRequest) (ports.DispatchReceipt, error) {
	return ports.DispatchReceipt{}, errors.New("fixture must not dispatch external work")
}
func (unusedDispatch) Reconcile(context.Context, ports.DispatchRequest) (ports.DispatchReceipt, error) {
	return ports.DispatchReceipt{}, errors.New("fixture must not dispatch external work")
}

type fixtureIdentityKey struct{}

func runtimePolicy() Policy {
	p := Policy{MaxRows: 500, MaxBytes: 2 << 20, HTTPMaxBytes: 1 << 20, MaxWorkerTenants: 2, MaxWorkerSlots: 32, Lease: time.Minute, StartupTimeout: 30 * time.Second, ReadinessTimeout: time.Second, RequestTimeout: 5 * time.Second, MaintenancePageSize: 5, MaintenancePagesPerTick: 1, MaintenanceInterval: time.Second, MaintenanceTimeout: time.Second,
		ReadHeaderTimeout: time.Second, HTTPIdleTimeout: time.Minute, HTTPMaxHeaderBytes: 16 << 10,
		Evaluation: mysqlexecution.EvaluationPolicy{Limits: service.ExecutionPreparationLimits{MaxItems: 10, MaxBytes: 2 << 20}, Aggregates: []string{"count", "sum", "avg"}}, Admission: mysqlexecution.AdmissionPolicy{ScheduledMaxAttempts: 3, AsyncMaxAttempts: 3}, Execution: mysql.ExecutionPolicy{Queues: map[ports.JobKind]mysql.DeliveryQueuePolicy{}, CallbackMaxAttempts: 3}, Effects: map[ports.JobKind]mysql.EffectQueuePolicy{}, Workers: map[ports.JobKind]mysqlexecution.EvaluationWorkerPolicy{}, Wait: mysqlapi.ExecutionWaitPolicy{MaxWindow: time.Second, PollInterval: 10 * time.Millisecond}, Scheduler: mysqlexecution.SchedulerPolicy{PageSize: 5, OccurrencesPerSchedule: 5, Interval: time.Second, TickTimeout: time.Second}}
	for _, kind := range allKinds {
		q := mysql.DeliveryQueuePolicy{Name: string(kind), MaxAttempts: 3}
		if kind == ports.JobScheduled || kind == ports.JobAsync || kind == ports.JobCallback {
			p.Execution.Queues[kind] = q
		} else {
			p.Effects[kind] = q
		}
		p.Workers[kind] = mysqlexecution.EvaluationWorkerPolicy{Concurrency: 1, PollInterval: 20 * time.Millisecond, PollJitter: time.Millisecond, ClaimTimeout: time.Second, HeartbeatInterval: 10 * time.Second, HeartbeatTimeout: time.Second, WorkTimeout: 30 * time.Second, DomainRetryBackoff: time.Second}
	}
	return p
}

func TestMySQLApplicationActualRoutesScopesReadinessAndWorkerLifecycle(t *testing.T) {
	// All foreign checks/identities below are explicitly FIXTURE PROVIDERS.
	// This tests decision composition, not AU/CM/SR provider implementations.
	db, database, definer, steps := testsupport.DecisionMySQLRuntimeDatabase(t)
	ctx := context.Background()
	tenant, parent := uuid.New(), uuid.New()
	schema, _ := mysqlstore.Identifier(database)
	var revoked atomic.Bool
	var platformDenied, platformMissing atomic.Bool
	var browserSession atomic.Pointer[mysql.TrustedDecisionSession]
	check := func(_ context.Context, tx *sql.Tx, id uuid.UUID) error {
		if tx == nil || id != tenant || revoked.Load() {
			return mysql.ErrDecisionAuthorizationDenied
		}
		return nil
	}
	authority := func(ctx context.Context, id uuid.UUID) (Authorities, error) {
		if id != tenant {
			return Authorities{}, mysql.ErrDecisionAuthorizationDenied
		}
		var mutate mysql.PlatformAuthorizer = func(ctx context.Context, tx *sql.Tx, id uuid.UUID, write bool) error {
			if !write || platformDenied.Load() {
				return mysql.ErrDecisionAuthorizationDenied
			}
			return check(ctx, tx, id)
		}
		if platformMissing.Load() {
			mutate = nil
		}
		return Authorities{TenantID: tenant,
			Session:           browserSession.Load(),
			PlatformMutations: mutate,
			Authoring: func(ctx context.Context, tx *sql.Tx, id uuid.UUID, _ mysql.AuthoringAction) error {
				return check(ctx, tx, id)
			},
			Reads: func(ctx context.Context, tx *sql.Tx, id uuid.UUID, write bool) error {
				if write {
					return mysql.ErrDecisionAuthorizationDenied
				}
				return check(ctx, tx, id)
			},
			Persist: func(ctx context.Context, tx *sql.Tx, id uuid.UUID, _ bool) error { return check(ctx, tx, id) },
			Executions: func(ctx context.Context, tx *sql.Tx, id uuid.UUID, _ mysql.ExecutionAction) error {
				return check(ctx, tx, id)
			},
			Worker: func(ctx context.Context, tx *sql.Tx, id uuid.UUID, _ ports.JobKind, _ mysql.EffectWorkerAction) error {
				return check(ctx, tx, id)
			}}, nil
	}
	owners := map[string]mysql.OwnerReadiness{}
	for _, name := range []string{"auth", "data_model", "case_management", "screening", "dispatch"} {
		owners[name] = func(context.Context) error { return nil }
	}
	readinessPolicy := mysql.RuntimeReadinessPolicy{ExpectedVersion: "8.4.8", PoolName: "fixture_api_and_workers", InspectionPoolName: "fixture_api_and_workers", ReservedConnections: 10, Pools: []mysqlstore.PoolBudget{{Name: "fixture_api_and_workers", Replicas: 1, MaxOpen: int64(db.Stats().MaxOpenConnections)}}}
	readiness, err := mysql.NewRuntimeReadiness(db, db, database, definer, steps, readinessPolicy, owners)
	if err != nil {
		t.Fatal(err)
	}
	models := &fixtureModel{tenant: tenant, model: ports.TenantModel{RevisionID: uuid.NewString(), RecordLookupField: "object_id", Tables: map[string]ports.TenantModelTable{}}}
	deps := Dependencies{DB: db, Database: database, Models: models, IndexPreparation: fixtureIndexPreparation{tenant}, IDs: fixtureIDs{}, Clock: fixtureClock{}, Readiness: readiness, ServiceAuthority: authority,
		Authenticate: func(r *http.Request) (context.Context, error) {
			if r.Header.Get("Authorization") != "Bearer fixture-only" {
				return nil, mysql.ErrDecisionAuthorizationDenied
			}
			return context.WithValue(r.Context(), fixtureIdentityKey{}, tenant), nil
		},
		RequestAuthority: func(ctx context.Context, id uuid.UUID) (Authorities, error) {
			if ctx.Value(fixtureIdentityKey{}) != tenant {
				return Authorities{}, mysql.ErrDecisionAuthorizationDenied
			}
			return authority(ctx, id)
		}, Dispatch: map[ports.JobKind]ports.ReceiptDispatcher{}}
	for _, kind := range dispatchKinds {
		deps.Dispatch[kind] = unusedDispatch{}
	}
	p := runtimePolicy()
	runtime, err := New(deps, p)
	if err != nil {
		t.Fatal(err)
	}
	// Input map mutation cannot redirect an already constructed worker.
	t.Run("explicit scoring deferral omits worker and authoring capability", func(t *testing.T) {
		deferredDeps := deps
		deferredDeps.Dispatch = cloneMap(deps.Dispatch)
		delete(deferredDeps.Dispatch, ports.JobScoring)
		policy := runtimePolicy()
		if _, err := New(deferredDeps, policy); !errors.Is(err, mysql.ErrOwnerContractUnavailable) {
			t.Fatal("implicit deferral accepted", err)
		}
		policy.Evaluation.DeferAdditionalScoring = true
		delete(policy.Workers, ports.JobScoring)
		r, err := New(deferredDeps, policy)
		if err != nil {
			t.Fatal(err)
		}
		scope, err := r.Scope(ctx, tenant, true)
		if err != nil {
			t.Fatal(err)
		}
		if scope.Endpoint.Scoring != nil {
			t.Fatal("scoring authoring available during deferral")
		}
		app, err := r.Start(ctx, []uuid.UUID{tenant})
		if err != nil {
			t.Fatal(err)
		}
		if len(app.workers) != 13 {
			t.Fatal("unexpected worker/maintenance count", len(app.workers))
		}
		for _, kind := range []ports.JobKind{ports.JobWorkflow, ports.JobOutbox} {
			incomplete := deferredDeps
			incomplete.Dispatch = cloneMap(deferredDeps.Dispatch)
			delete(incomplete.Dispatch, kind)
			if _, err := New(incomplete, policy); !errors.Is(err, mysql.ErrOwnerContractUnavailable) {
				t.Fatal("unrelated missing provider accepted", kind, err)
			}
		}
	})
	p.Execution.Queues[ports.JobScheduled] = mysql.DeliveryQueuePolicy{Name: "changed", MaxAttempts: 9}
	if runtime.policy.Execution.Queues[ports.JobScheduled].Name == "changed" {
		t.Fatal("mutable configuration retained")
	}
	missing := deps
	missing.Dispatch = cloneMap(deps.Dispatch)
	delete(missing.Dispatch, ports.JobCallback)
	if _, err := New(missing, runtimePolicy()); !errors.Is(err, mysql.ErrOwnerContractUnavailable) {
		t.Fatal("missing delivery provider accepted", err)
	}
	bad := deps
	bad.ServiceAuthority = func(context.Context, uuid.UUID) (Authorities, error) { return Authorities{TenantID: uuid.New()}, nil }
	r, err := New(bad, runtimePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Start(ctx, []uuid.UUID{tenant}); !errors.Is(err, mysql.ErrTenantScope) {
		t.Fatal("foreign scope accepted", err)
	}
	closedOwners := cloneMap(owners)
	closedOwners["auth"] = func(context.Context) error { return mysql.ErrOwnerContractUnavailable }
	closedReadiness, err := mysql.NewRuntimeReadiness(db, db, database, definer, steps, readinessPolicy, closedOwners)
	if err != nil {
		t.Fatal(err)
	}
	closedDeps := deps
	closedDeps.Readiness = closedReadiness
	closedRuntime, err := New(closedDeps, runtimePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := closedRuntime.Start(ctx, []uuid.UUID{tenant}); !errors.Is(err, mysql.ErrOwnerContractUnavailable) {
		t.Fatal("unimplemented auth owner started application", err)
	}
	tooMany := runtimePolicy()
	tooMany.MaxWorkerSlots = 14
	limited, err := New(deps, tooMany)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := limited.Start(ctx, []uuid.UUID{tenant}); err == nil {
		t.Fatal("worker slots exceeded configured inventory")
	}
	a, err := runtime.Start(ctx, []uuid.UUID{tenant})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.workers) != 15 {
		t.Fatal("missing delivery/scheduler/maintenance loops", len(a.workers))
	}
	server, err := a.HTTPServer(":8082")
	if err != nil || server.ReadTimeout != 6*time.Second || server.ReadHeaderTimeout != time.Second || server.MaxHeaderBytes != 16<<10 {
		t.Fatal("HTTP deadlines not bound", server, err)
	}
	if _, err := a.HTTPServer("missing-port"); err == nil {
		t.Fatal("invalid listener accepted")
	}
	t.Cleanup(func() {
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := a.Shutdown(stop); err != nil {
			t.Error(err)
		}
	})
	request := func(method, path, body string, authenticated bool, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if authenticated {
			req.Header.Set("Authorization", "Bearer fixture-only")
		}
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, rec.Code, rec.Body.String())
		}
		return rec
	}
	base := "/v1/tenants/" + tenant.String()
	request("GET", base+"/async-decision-executions", "", false, 401)
	request("GET", base+"/async-decision-executions", "", true, 200)
	request("GET", "/v1/tenants/"+uuid.NewString()+"/async-decision-executions", "", true, 403)
	request("GET", "/readyz", "", false, 200)
	if _, err := db.Exec("RENAME TABLE " + schema + ".decision_engine_execution_job_deliveries TO " + schema + ".fixture_hidden_queue"); err != nil {
		t.Fatal(err)
	}
	request("GET", "/readyz", "", false, 503)
	if _, err := db.Exec("RENAME TABLE " + schema + ".fixture_hidden_queue TO " + schema + ".decision_engine_execution_job_deliveries"); err != nil {
		t.Fatal(err)
	}
	request("GET", "/readyz", "", false, 200)

	// Owner publication fixture, not an implementation of data-model DDL.
	table, field, version := uuid.New(), uuid.New(), uuid.New()
	column := "field_x_" + strings.ReplaceAll(field.String(), "-", "") + "_value"
	physical := "tenant_x_" + strings.ReplaceAll(table.String(), "-", "")
	for _, q := range []string{
		"CREATE TABLE " + schema + ".data_model_physical_tables(tenant_id BINARY(16),table_id BINARY(16),physical_name VARCHAR(64),published_version BINARY(16),epoch BIGINT,write_state VARCHAR(10),active_operation BINARY(16),PRIMARY KEY(tenant_id,table_id))",
		"CREATE TABLE " + schema + ".data_model_ingestion_contracts(tenant_id BINARY(16),table_id BINARY(16),table_name LONGTEXT,version BINARY(16),payload JSON,PRIMARY KEY(tenant_id,table_id))",
		"CREATE TABLE " + schema + ".data_model_physical_fields(tenant_id BINARY(16),table_id BINARY(16),field_id BINARY(16),component VARCHAR(7),physical_name VARCHAR(64),PRIMARY KEY(tenant_id,table_id,field_id,component))",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	doc, _ := json.Marshal(map[string]any{"id": table, "name": "wallets", "fields": map[string]any{"object_id": map[string]any{"id": field, "name": "object_id", "data_type": "string", "nullable": false, "is_unique": true}}})
	if _, err := db.Exec("INSERT INTO "+schema+".data_model_physical_tables VALUES(?,?,?,?,1,'open',NULL)", mysqlstore.UUID(tenant), mysqlstore.UUID(table), physical, mysqlstore.UUID(version)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO "+schema+".data_model_ingestion_contracts VALUES(?,?,'wallets',?,?)", mysqlstore.UUID(tenant), mysqlstore.UUID(table), mysqlstore.UUID(version), mysqlstore.JSON(doc)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO "+schema+".data_model_physical_fields VALUES(?,?,?,'value',?)", mysqlstore.UUID(tenant), mysqlstore.UUID(table), mysqlstore.UUID(field), column); err != nil {
		t.Fatal(err)
	}
	models.model.Tables["wallets"] = ports.TenantModelTable{ID: table.String(), Name: "wallets", PublishedVersion: version.String(), Fields: map[string]ports.TenantModelField{"object_id": {Name: "object_id", Type: "string"}}}
	t.Run("ten remaining APIs", func(t *testing.T) { testTenAPIs(t, a.Handler(), base, db, schema, &revoked) })
	t.Run("ten history APIs", func(t *testing.T) { testHistoryAPIs(t, a.Handler(), base, db, database, &revoked) })
	t.Run("ten administration APIs", func(t *testing.T) { testAdministrationAPIs(t, a.Handler(), base, db, schema, &revoked) })
	t.Run("ten custom list APIs", func(t *testing.T) { testCustomListAPIs(t, a.Handler(), base, db, schema, &revoked) })
	t.Run("ten platform tasks", func(t *testing.T) {
		testPlatformAPIs(t, a.Handler(), base, db, schema, &revoked, &platformDenied, &platformMissing)
	})
	t.Run("browser workflow APIs", func(t *testing.T) { testBrowserWorkflowAPIs(t, a.Handler(), base, db, schema, &browserSession) })
	t.Run("authoring HTTP lifecycle", func(t *testing.T) {
		root := base + "/scenarios"
		decodeID := func(rec *httptest.ResponseRecorder, key string) string {
			t.Helper()
			var body map[string]struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body[key].ID == "" {
				t.Fatalf("missing %s: %s", key, rec.Body.String())
			}
			return body[key].ID
		}
		request("POST", root, `{"name":"HTTP authoring","trigger_object_type":"wallets","tenant_id":"forged"}`, true, 400)
		request("POST", root, `{"name":"HTTP authoring","trigger_object_type":"wallets"}`, false, 401)
		id := decodeID(request("POST", root, `{"name":"HTTP authoring","trigger_object_type":"wallets"}`, true, 201), "scenario")
		path := root + "/" + id
		request("POST", root, `{"name":"HTTP authoring","trigger_object_type":"wallets"}`, true, 409)
		request("GET", path, "", true, 200)
		request("GET", root, "", true, 200)
		request("PUT", path, `{"name":"HTTP updated","description":"edited","trigger_object_type":"wallets"}`, true, 200)
		request("POST", path+"/iterations", `{"status":"committed"}`, true, 400)
		it := decodeID(request("POST", path+"/iterations", "", true, 201), "iteration")
		ip := path + "/iterations/" + it
		request("PUT", ip, `{}`, true, 422)
		request("POST", ip+"/rules", `{"name":"missing formula"}`, true, 422)
		request("GET", path+"/iterations/metadata", "", true, 200)
		request("GET", path+"/iterations", "", true, 200)
		request("GET", ip, "", true, 200)
		invalid := `{"trigger_formula":{"constant":5},"score_review_threshold":1,"score_block_and_review_threshold":10,"score_decline_threshold":20}`
		request("PUT", ip, invalid, true, 200)
		request("GET", path+"/publications/preparation?iteration_id="+it, "", true, 422)
		request("POST", ip+"/commit", "{}", true, 422)
		valid := `{"trigger_formula":{"constant":true},"score_review_threshold":1,"score_block_and_review_threshold":10,"score_decline_threshold":20}`
		request("PUT", ip, valid, true, 200)
		ruleBody := `{"name":"test","formula":{"constant":true},"score_modifier":12,"rule_group":"risk","stable_rule_id":"stable-http"}`
		rid := decodeID(request("POST", ip+"/rules", ruleBody, true, 201), "rule")
		request("PUT", ip+"/rules/"+rid, ruleBody, true, 200)
		request("GET", ip+"/rules", "", true, 200)
		request("GET", path+"/publications/preparation?iteration_id="+it, "", true, 200)
		request("POST", path+"/publications/preparation", `{"iteration_id":"`+it+`"}`, true, 202)
		request("GET", path+"/rule-groups", "", true, 200)
		request("GET", root+"/"+parent.String()+"/iterations/"+it+"/rules", "", true, 404)
		revoked.Store(true)
		request("GET", path, "", true, 403)
		request("POST", ip+"/commit", "", true, 403)
		revoked.Store(false)
		request("POST", ip+"/commit", "{}", true, 200)
		request("PUT", ip, valid, true, 409)
		request("DELETE", ip+"/rules/"+rid, "", true, 409)
		clone := decodeID(request("POST", ip+"/draft", "", true, 201), "iteration")
		cp := path + "/iterations/" + clone
		var rules struct {
			Rules []struct {
				ID     string `json:"id"`
				Stable string `json:"stable_rule_id"`
			} `json:"rules"`
		}
		res := request("GET", cp+"/rules", "", true, 200)
		if err := json.Unmarshal(res.Body.Bytes(), &rules); err != nil || len(rules.Rules) != 1 || rules.Rules[0].ID == rid || rules.Rules[0].Stable != "stable-http" {
			t.Fatal("clone lost rule identity", res.Body.String(), err)
		}
		request("DELETE", cp+"/rules/"+rules.Rules[0].ID, "", true, 204)
		request("POST", path+"/copy", `{"name":"Copied authoring graph"}`, false, 401)
		request("POST", path+"/copy", `{"name":"copy","actor":"forged"}`, true, 400)
		revoked.Store(true)
		request("POST", path+"/copy", `{"name":"copy"}`, true, 403)
		revoked.Store(false)
		copied := decodeID(request("POST", path+"/copy", `{"name":"Copied authoring graph"}`, true, 201), "scenario")
		copyPath := root + "/" + copied
		var copiedIterations struct {
			Iterations []struct {
				ID      string `json:"id"`
				Version int    `json:"version"`
			} `json:"iterations"`
		}
		res = request("GET", copyPath+"/iterations", "", true, 200)
		if err := json.Unmarshal(res.Body.Bytes(), &copiedIterations); err != nil || len(copiedIterations.Iterations) != 2 {
			t.Fatal("incomplete copied iterations", res.Body.String(), err)
		}
		for _, copyIteration := range copiedIterations.Iterations {
			if copyIteration.ID == it || copyIteration.ID == clone {
				t.Fatal("source iteration identity retained")
			}
			if copyIteration.Version == 1 {
				res = request("GET", copyPath+"/iterations/"+copyIteration.ID+"/rules", "", true, 200)
				if err := json.Unmarshal(res.Body.Bytes(), &rules); err != nil || len(rules.Rules) != 1 || rules.Rules[0].ID == rid || rules.Rules[0].Stable == "stable-http" {
					t.Fatal("copied rule binding", res.Body.String(), err)
				}
			}
		}
		request("DELETE", copyPath, "", true, 204)
		request("DELETE", path, "", true, 204)
		request("GET", path, "", true, 404)
	})
	auth, err := authority(ctx, tenant)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := mysql.NewAuthoringStore(db, database, tenant, 500, 2<<20, auth.Authoring)
	if err != nil {
		t.Fatal(err)
	}
	iteration := uuid.New()
	at := fixtureClock{}.Now()
	review, block, decline := 10, 20, 30
	if err := owner.Mutate(ctx, func(r mysql.AuthoringRepositories) error {
		if _, err := r.Scenarios.Create(ctx, scenario.Scenario{ID: parent.String(), TenantID: tenant.String(), Name: "Runtime fixture", Description: "test", TriggerObjectType: "wallets", CreatedAt: at, UpdatedAt: at}); err != nil {
			return err
		}
		version, err := r.Iterations.NextVersion(ctx, tenant.String(), parent.String())
		if err != nil {
			return err
		}
		if _, err := r.Iterations.Create(ctx, scenario.Iteration{ID: iteration.String(), TenantID: tenant.String(), ScenarioID: parent.String(), Version: version, Status: scenario.IterationStatusDraft, TriggerFormula: []byte(`{"constant":true}`), ScoreReviewThreshold: &review, ScoreBlockAndReviewThreshold: &block, ScoreDeclineThreshold: &decline, CreatedAt: at}); err != nil {
			return err
		}
		_, err = r.Iterations.Commit(ctx, tenant.String(), parent.String(), iteration.String(), at)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	publicationPath := base + "/scenarios/" + parent.String() + "/publications"
	t.Run("bounded publication preparation API", func(t *testing.T) {
		path := publicationPath + "/preparation"
		query := "?iteration_id=" + iteration.String()
		body := `{"iteration_id":"` + iteration.String() + `"}`
		request("GET", path+query, "", false, 401)
		request("GET", path, "", true, 400)
		request("GET", path+query+"&iteration_id="+iteration.String(), "", true, 400)
		request("POST", path, `{"iteration_id":"`+iteration.String()+`","actor":"forged"}`, true, 400)
		revoked.Store(true)
		request("GET", path+query, "", true, 403)
		request("POST", path, body, true, 403)
		revoked.Store(false)
		request("GET", path+"?iteration_id="+uuid.NewString(), "", true, 404)
		for _, method := range []string{"GET", "POST"} {
			url, payload, want := path+query, "", 200
			if method == "POST" {
				url, payload, want = path, body, 202
			}
			rec := request(method, url, payload, true, want)
			var response struct {
				Preparation struct {
					Required bool `json:"preparation_required"`
					Finished bool `json:"preparation_finished"`
				} `json:"preparation"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || response.Preparation.Required || !response.Preparation.Finished {
				t.Fatal(rec.Body.String(), err)
			}
		}
	})
	publishBody := `{"action":"publish","iteration_id":"` + iteration.String() + `"}`
	unpublishBody := `{"action":"unpublish","iteration_id":"` + iteration.String() + `"}`
	request("POST", publicationPath, publishBody, false, 401)
	request("POST", publicationPath, `{"action":"publish","action":"unpublish","iteration_id":"`+iteration.String()+`"}`, true, 400)
	request("POST", publicationPath, `{"action":"publish","iteration_id":"`+iteration.String()+`","actor":"forged"}`, true, 400)
	request("POST", publicationPath, `{"action":"publish","iteration_id":"invalid"}`, true, 400)
	request("POST", publicationPath+"?ignored=1", publishBody, true, 400)
	request("POST", publicationPath, unpublishBody, true, 409)
	request("POST", publicationPath, publishBody, true, 200)
	assertPublicationCount := func(want int) {
		t.Helper()
		var result struct {
			Publications []json.RawMessage `json:"publications"`
		}
		rec := request("GET", publicationPath, "", true, 200)
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || len(result.Publications) != want {
			t.Fatalf("publication count: %s, want %d: %v", rec.Body.String(), want, err)
		}
	}
	assertPublicationCount(1)
	request("GET", "/v1/tenants/"+uuid.NewString()+"/scenarios/"+parent.String()+"/publications", "", true, 403)
	request("GET", base+"/scenarios/"+uuid.NewString()+"/publications", "", true, 404)
	if _, err := db.Exec("UPDATE " + schema + ".data_model_physical_tables SET write_state='closed'"); err != nil {
		t.Fatal(err)
	}
	request("POST", publicationPath, publishBody, true, 409)
	assertPublicationCount(1)
	// Unpublication is still available when model preparation is unavailable.
	request("POST", publicationPath, unpublishBody, true, 200)
	assertPublicationCount(2)
	if _, err := db.Exec("UPDATE " + schema + ".data_model_physical_tables SET write_state='open'"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TRIGGER " + schema + ".fixture_publication_failure BEFORE INSERT ON " + schema + ".decision_engine_scenario_publications FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='private fixture failure'"); err != nil {
		t.Fatal(err)
	}
	failed := request("POST", publicationPath, publishBody, true, 500)
	if strings.Contains(failed.Body.String(), "private fixture") {
		t.Fatal("database error leaked")
	}
	assertPublicationCount(2)
	// The live-link update must roll back with the failed history insert.
	request("POST", publicationPath, unpublishBody, true, 409)
	if _, err := db.Exec("DROP TRIGGER " + schema + ".fixture_publication_failure"); err != nil {
		t.Fatal(err)
	}
	revoked.Store(true)
	request("POST", publicationPath, publishBody, true, 403)
	request("GET", publicationPath, "", true, 403)
	revoked.Store(false)
	request("POST", publicationPath, publishBody, true, 200)
	assertPublicationCount(3)
	t.Run("live scenario copy and publication rollback", func(t *testing.T) {
		copyPath := base + "/scenarios/" + parent.String() + "/copy"
		if _, err := db.Exec("CREATE TRIGGER " + schema + ".fixture_copy_publication_failure BEFORE INSERT ON " + schema + ".decision_engine_scenario_publications FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='copy publication failure'"); err != nil {
			t.Fatal(err)
		}
		request("POST", copyPath, `{"name":"Must rollback copy"}`, true, 500)
		if _, err := db.Exec("DROP TRIGGER " + schema + ".fixture_copy_publication_failure"); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM "+schema+".decision_engine_scenarios WHERE tenant_id=? AND name='Must rollback copy'", mysqlstore.UUID(tenant)).Scan(&count); err != nil || count != 0 {
			t.Fatal("partial scenario survived", count, err)
		}
		if _, err := db.Exec("UPDATE " + schema + ".data_model_physical_tables SET write_state='closed'"); err != nil {
			t.Fatal(err)
		}
		request("POST", copyPath, `{"name":"Closed-model copy"}`, true, 409)
		if _, err := db.Exec("UPDATE " + schema + ".data_model_physical_tables SET write_state='open'"); err != nil {
			t.Fatal(err)
		}
		rec := request("POST", copyPath, `{"name":"Live copied scenario"}`, true, 201)
		var result struct {
			Scenario struct {
				ID   string  `json:"id"`
				Live *string `json:"live_iteration_id"`
			} `json:"scenario"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || result.Scenario.Live == nil || *result.Scenario.Live == iteration.String() {
			t.Fatal("live copy", rec.Body.String(), err)
		}
		request("POST", base+"/scenarios/"+result.Scenario.ID+"/publications", `{"action":"unpublish","iteration_id":"`+*result.Scenario.Live+`"}`, true, 200)
		request("DELETE", base+"/scenarios/"+result.Scenario.ID, "", true, 204)
	})
	schedulePath := base + "/scenarios/" + parent.String() + "/recurring-schedule"
	scheduleID := uuid.New()
	request("PUT", schedulePath, `{"schedule_id":"`+scheduleID.String()+`","expected_schedule_id":"00000000-0000-0000-0000-000000000000","expected_watermark_version":0,"calendar":{"version":1,"frequency":"daily","timezone":"UTC","time_of_day":"09:30","gap_policy":"skip","overlap_policy":"first"},"start_local":"2099-10-31T09:30","missed_policy":"coalesce","candidate_limit":1}`, true, 200)
	request("GET", schedulePath, "", true, 200)
	request("DELETE", schedulePath, `{"expected_schedule_id":"`+scheduleID.String()+`","expected_watermark_version":1}`, true, 409)
	request("DELETE", schedulePath, `{"expected_schedule_id":"`+scheduleID.String()+`","expected_watermark_version":0}`, true, 200)
	request("GET", schedulePath+"/"+scheduleID.String(), "", true, 200)
	request("POST", base+"/scenarios/"+parent.String()+"/scheduled-executions", `{"scheduled_for":"2099-10-31T09:30:00Z","candidate_limit":1}`, true, 202)
	request("GET", base+"/scenarios/"+parent.String()+"/scheduled-executions?status=pending&limit=1", "", true, 200)
	revoked.Store(true)
	request("GET", base+"/async-decision-executions", "", true, 403)
	revoked.Store(false)
	finished := make(chan error, 1)
	go func() { finished <- a.RunWorkers(ctx, func(context.Context, error) error { return nil }) }()
	// Shutdown may race startup registration; it must not start later work.
	stop, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := a.Shutdown(stop); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err == nil {
		t.Fatal("workers reported successful completion")
	}
	request("GET", "/readyz", "", false, 503)
	request("GET", "/v1/rule-functions", "", true, 503)
	request("GET", base+"/async-decision-executions", "", true, 503)
	if _, err := db.Exec("DROP TRIGGER " + schema + ".decision_engine_dispatch_intents_update"); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Start(ctx, []uuid.UUID{tenant}); !errors.Is(err, migrations.ErrDrift) {
		t.Fatal("missing integrity guard accepted", err)
	}
}
