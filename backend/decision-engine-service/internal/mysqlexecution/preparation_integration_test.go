package mysqlexecution

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/testsupport"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type ids struct{}

func (ids) New() uuid.UUID { return uuid.New() }

type clock struct{}

func (clock) Now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

type modelProvider struct {
	ports.DataModelReader
	model  ports.TenantModel
	tenant uuid.UUID
	onRead func()
	calls  int
	mu     sync.Mutex
}

func (m *modelProvider) GetTenantModel(_ context.Context, t string) (ports.TenantModel, error) {
	if t != m.tenant.String() {
		return ports.TenantModel{}, mysql.ErrTenantScope
	}
	m.mu.Lock()
	m.calls++
	onRead := m.onRead
	m.mu.Unlock()
	if onRead != nil {
		onRead()
	}
	return m.model, nil
}

type preparationFixture struct {
	db                                  *sql.DB
	database, schema                    string
	physical                            string
	tenant, parent, old, table, version uuid.UUID
	authoring                           *mysql.AuthoringStore
	executions                          *mysql.ExecutionStore
	unit                                *mysql.DecisionUnitOfWorkStore
	preparer                            *Preparer
	provider                            *modelProvider
	revoked                             atomic.Bool
	reads                               mysql.EvaluationReaders
}

func setupPreparation(t *testing.T) *preparationFixture {
	t.Helper()
	ctx := context.Background()
	db, name := testsupport.DecisionMySQLDatabase(t)
	schema, _ := mysqlstore.Identifier(name)
	f := &preparationFixture{db: db, database: name, schema: schema, tenant: uuid.New(), parent: uuid.New(), table: uuid.New(), version: uuid.New()}
	allow := func(context.Context, *sql.Tx, uuid.UUID, mysql.AuthoringAction) error { return nil }
	a, err := mysql.NewAuthoringStore(db, name, f.tenant, 500, 2<<20, allow)
	if err != nil {
		t.Fatal(err)
	}
	f.authoring = a
	at := clock{}.Now()
	if err := a.Mutate(ctx, func(r mysql.AuthoringRepositories) error {
		_, err := r.Scenarios.Create(ctx, scenario.Scenario{ID: f.parent.String(), TenantID: f.tenant.String(), Name: "Pinned owner", Description: "test", TriggerObjectType: "wallets", CreatedAt: at, UpdatedAt: at})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	f.old = f.publish(t, 15)
	// Data-model OWNER CONTRACT FIXTURE only: declared published registry and
	// physical mapping, not a replacement implementation of the owner's DDL.
	for _, q := range []string{
		"CREATE TABLE " + schema + ".data_model_physical_tables(tenant_id BINARY(16),table_id BINARY(16),physical_name VARCHAR(64),published_version BINARY(16),epoch BIGINT,write_state VARCHAR(10),active_operation BINARY(16),PRIMARY KEY(tenant_id,table_id))",
		"CREATE TABLE " + schema + ".data_model_ingestion_contracts(tenant_id BINARY(16),table_id BINARY(16),table_name LONGTEXT,version BINARY(16),payload JSON,PRIMARY KEY(tenant_id,table_id))",
		"CREATE TABLE " + schema + ".data_model_physical_fields(tenant_id BINARY(16),table_id BINARY(16),field_id BINARY(16),component VARCHAR(7),physical_name VARCHAR(64),PRIMARY KEY(tenant_id,table_id,field_id,component))",
		"CREATE TABLE " + schema + ".data_model_physical_lookups(tenant_id BINARY(16),table_id BINARY(16),field_id BINARY(16),lookup_kind VARCHAR(20),physical_key_column VARCHAR(64),PRIMARY KEY(tenant_id,table_id))",
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	mt := ports.TenantModelTable{ID: f.table.String(), Name: "wallets", PublishedVersion: f.version.String(), Fields: map[string]ports.TenantModelField{}}
	fields := map[string]any{}
	columns := map[string]string{}
	definition := []string{}
	var objectID uuid.UUID
	for _, field := range []struct{ name, kind, column string }{{"object_id", "string", "object_value"}, {"amount", "int", "amount_value"}, {"updated_at", "timestamp", "updated_value"}} {
		id := uuid.New()
		column := "field_x_" + strings.ReplaceAll(id.String(), "-", "") + "_value"
		quoted, err := mysqlstore.Identifier(column)
		if err != nil {
			t.Fatal(err)
		}
		columns[field.name] = quoted
		sqlType := "INT NOT NULL"
		if field.kind == "string" {
			sqlType = "VARCHAR(100) COLLATE utf8mb4_0900_bin PRIMARY KEY"
		}
		if field.kind == "timestamp" {
			sqlType = "DATETIME(6) NOT NULL"
		}
		definition = append(definition, quoted+" "+sqlType)
		if field.name == "object_id" {
			objectID = id
		}
		mt.Fields[field.name] = ports.TenantModelField{Name: field.name, Type: field.kind}
		fields[field.name] = map[string]any{"id": id, "name": field.name, "data_type": field.kind, "nullable": false, "is_unique": field.name == "object_id"}
		if _, err := db.ExecContext(ctx, "INSERT INTO "+schema+".data_model_physical_fields VALUES(?,?,?,?,?)", mysqlstore.UUID(f.tenant), mysqlstore.UUID(f.table), mysqlstore.UUID(id), "value", column); err != nil {
			t.Fatal(err)
		}
	}
	f.physical = "tenant_test_wallets_" + strings.ReplaceAll(f.table.String(), "-", "")
	physical, err := mysqlstore.Identifier(f.physical)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+schema+"."+physical+"("+strings.Join(definition, ",")+",object_key BINARY(16) NOT NULL UNIQUE)"); err != nil {
		t.Fatal(err)
	}
	model := ports.TenantModel{RevisionID: "published-model-v1", RecordLookupField: "object_id", Tables: map[string]ports.TenantModelTable{"wallets": mt}}
	raw, _ := json.Marshal(map[string]any{"id": f.table, "name": "wallets", "fields": fields})
	if _, err := db.ExecContext(ctx, "INSERT INTO "+schema+".data_model_physical_tables VALUES(?,?,?,?,1,'open',NULL)", mysqlstore.UUID(f.tenant), mysqlstore.UUID(f.table), f.physical, mysqlstore.UUID(f.version)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO "+schema+".data_model_ingestion_contracts VALUES(?,?,'wallets',?,?)", mysqlstore.UUID(f.tenant), mysqlstore.UUID(f.table), mysqlstore.UUID(f.version), mysqlstore.JSON(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO "+schema+".data_model_physical_lookups VALUES(?,?,?,'object_id_exact_v1','object_key')", mysqlstore.UUID(f.tenant), mysqlstore.UUID(f.table), mysqlstore.UUID(objectID)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "CREATE FUNCTION "+schema+".data_model_record_lookup_v1(t BINARY(16),tab BINARY(16),v BINARY(16),value LONGTEXT) RETURNS BINARY(16) READS SQL DATA BEGIN DECLARE k BINARY(16); SELECT object_key INTO k FROM "+schema+"."+physical+" WHERE CAST("+columns["object_id"]+" AS BINARY)=CAST(value AS BINARY) AND t=X'"+strings.ReplaceAll(f.tenant.String(), "-", "")+"' AND tab=X'"+strings.ReplaceAll(f.table.String(), "-", "")+"' AND v=X'"+strings.ReplaceAll(f.version.String(), "-", "")+"'; RETURN k; END"); err != nil {
		t.Fatal(err)
	}
	f.provider = &modelProvider{model: model, tenant: f.tenant}
	readAuthority := func(context.Context, *sql.Tx, uuid.UUID, bool) error {
		if f.revoked.Load() {
			return mysql.ErrDecisionAuthorizationDenied
		}
		return nil
	}
	reader, err := mysql.NewTenantDataReader(db, name, f.tenant, 500, 2<<20, f.provider, readAuthority)
	if err != nil {
		t.Fatal(err)
	}
	queues := map[ports.JobKind]mysql.DeliveryQueuePolicy{}
	for _, kind := range []ports.JobKind{ports.JobScheduled, ports.JobAsync, ports.JobCallback} {
		queues[kind] = mysql.DeliveryQueuePolicy{Name: string(kind), MaxAttempts: 3}
	}
	f.executions, err = mysql.NewExecutionStore(db, name, f.tenant, 500, 2<<20, func(ctx context.Context, tx *sql.Tx, tenant uuid.UUID, action mysql.ExecutionAction) error {
		if f.revoked.Load() || tenant != f.tenant {
			return mysql.ErrDecisionAuthorizationDenied
		}
		if action != mysql.ExecutionRead {
			var ro int
			if err := tx.QueryRowContext(ctx, "SELECT @@transaction_read_only").Scan(&ro); err != nil {
				return err
			}
			if ro != 0 {
				return errors.New("write-purpose authority incorrectly called in read-only snapshot")
			}
		}
		return nil
	}, mysql.ExecutionPolicy{Queues: queues, CallbackMaxAttempts: 3})
	if err != nil {
		t.Fatal(err)
	}
	effectPolicies := map[ports.JobKind]mysql.EffectQueuePolicy{}
	for _, kind := range []ports.JobKind{ports.JobWorkflow, ports.JobScreening, ports.JobScoring, ports.JobOutbox} {
		effectPolicies[kind] = mysql.EffectQueuePolicy{Name: string(kind), MaxAttempts: 3}
	}
	effects, err := mysql.NewQueuedEffectStore(db, name, f.tenant, 500, 2<<20, readAuthority, nil, effectPolicies)
	if err != nil {
		t.Fatal(err)
	}
	f.unit, err = mysql.NewDecisionUnitOfWorkStore(effects, f.executions)
	if err != nil {
		t.Fatal(err)
	}
	config, err := mysql.NewConfigurationStore(db, name, f.tenant, 500, 2<<20, allow)
	if err != nil {
		t.Fatal(err)
	}
	history, err := mysql.NewDecisionStore(db, name, f.tenant, 500, 2<<20, readAuthority)
	if err != nil {
		t.Fatal(err)
	}
	platform, err := mysql.NewPlatformStore(db, name, f.tenant, 500, 2<<20, readAuthority)
	if err != nil {
		t.Fatal(err)
	}
	lists, err := mysql.NewCustomListStore(db, name, f.tenant, 500, 2<<20, allow)
	if err != nil {
		t.Fatal(err)
	}
	f.reads, err = mysql.NewEvaluationReaders(f.executions, config, history, platform, lists)
	if err != nil {
		t.Fatal(err)
	}
	f.preparer, err = NewTargetPreparer(f.executions, reader, f.reads, ids{}, clock{}, nil, EvaluationPolicy{Limits: service.ExecutionPreparationLimits{MaxItems: 10, MaxBytes: 2 << 20}, PublishOutboxEvents: true, Aggregates: []string{"count", "sum", "avg"}, RuleConcurrency: 1, ScenarioConcurrency: 1, AggregateConcurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *preparationFixture) publish(t *testing.T, score int) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	at := clock{}.Now()
	a, b, c := 10, 20, 30
	if err := f.authoring.Mutate(ctx, func(r mysql.AuthoringRepositories) error {
		version, err := r.Iterations.NextVersion(ctx, f.tenant.String(), f.parent.String())
		if err != nil {
			return err
		}
		if _, err := r.Iterations.Create(ctx, scenario.Iteration{ID: id.String(), TenantID: f.tenant.String(), ScenarioID: f.parent.String(), Version: version, Status: scenario.IterationStatusDraft, TriggerFormula: []byte(`{"constant":true}`), ScoreReviewThreshold: &a, ScoreBlockAndReviewThreshold: &b, ScoreDeclineThreshold: &c, CreatedAt: at}); err != nil {
			return err
		}
		if _, err := r.Rules.Create(ctx, scenario.Rule{ID: uuid.NewString(), TenantID: f.tenant.String(), IterationID: id.String(), Name: "Pinned score", StableRuleID: "score", DisplayOrder: 1, Formula: []byte(`{"constant":true}`), ScoreModifier: score, CreatedAt: at, UpdatedAt: at}); err != nil {
			return err
		}
		_, err = r.Iterations.Commit(ctx, f.tenant.String(), f.parent.String(), id.String(), at)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.authoring.Publish(ctx, mysql.PublicationInput{ScenarioID: f.parent, IterationID: id, EventID: uuid.New(), ReplacedEventID: uuid.New()}, func(context.Context, *sql.Tx, mysql.PublicationSnapshot) error { return nil }); err != nil {
		t.Fatal(err)
	}
	return id
}
func (f *preparationFixture) running(t *testing.T, items []service.DecisionEvaluationRequest, limit int) mysql.ExecutionLease {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	at := clock{}.Now()
	key := id.String()
	body, _ := json.Marshal(service.ScheduledExecutionRequest{Items: items, CandidateLimit: limit, IdempotencyKey: key})
	if err := f.executions.Run(ctx, mysql.ExecutionRequest, func(r mysql.ExecutionRepositories) error {
		_, err := r.Scheduled.Create(ctx, execution.ScheduledExecution{ID: id.String(), TenantID: f.tenant.String(), ScenarioID: f.parent.String(), ScenarioIterationID: f.old.String(), Source: execution.SourceManual, Status: execution.StatusPending, IdempotencyKey: key, MaxAttempts: 3, ScheduledFor: at, CreatedAt: at, RequestBody: body})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	lease := mysql.ExecutionLease{Token: uuid.New(), Identity: mysqljobs.Identity{Version: 1, Kind: ports.JobScheduled, TenantID: f.tenant, ResourceID: id, DomainAttempt: 1}, TransportAttempt: 1}
	var receipt mysqlstore.UUID
	if err := f.db.QueryRowContext(ctx, "SELECT id FROM "+f.schema+".decision_engine_execution_job_receipts WHERE tenant_id=? AND kind=? AND resource_id=?", mysqlstore.UUID(f.tenant), string(ports.JobScheduled), mysqlstore.UUID(id)).Scan(&receipt); err != nil {
		t.Fatal(err)
	}
	lease.ID = uuid.UUID(receipt)
	if _, err := f.db.ExecContext(ctx, "UPDATE "+f.schema+".decision_engine_execution_job_deliveries SET state='running',attempt=1,lease_token=?,lease_until=UTC_TIMESTAMP(6)+INTERVAL 60 SECOND WHERE tenant_id=? AND id=?", mysqlstore.UUID(lease.Token), mysqlstore.UUID(f.tenant), receipt); err != nil {
		t.Fatal(err)
	}
	if err := f.executions.Run(ctx, mysql.ExecutionRun, func(r mysql.ExecutionRepositories) error { _, err := r.Scheduled.Start(ctx, lease); return err }); err != nil {
		t.Fatal(err)
	}
	return lease
}
func (f *preparationFixture) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow("SELECT COUNT(*) FROM " + f.schema + "." + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestMySQLRequestPreparationPublishedBindingsAndIsolation(t *testing.T) {
	f := setupPreparation(t)
	ctx := context.Background()
	items := []service.DecisionEvaluationRequest{{ObjectID: " exact ", ObjectType: "wallets", Fields: map[string]any{"amount": 10}}}
	for _, parent := range []string{"", f.parent.String()} {
		prepared, err := f.preparer.PrepareRequest(ctx, f.tenant.String(), parent, "wallets", items)
		if err != nil || prepared.Batch().Len() != 1 {
			t.Fatal(prepared, err)
		}
		_, pinned := prepared.PinnedScenario()
		if pinned != (parent != "") {
			t.Fatal("wrong scenario binding")
		}
	}
	if f.provider.calls != 2 {
		t.Fatal("stale model cache used", f.provider.calls)
	}
	if f.count(t, "decision_engine_decisions") != 0 || f.count(t, "decision_engine_execution_job_receipts") != 0 {
		t.Fatal("preparation performed owner writes")
	}
	if _, err := f.preparer.PrepareRequest(ctx, uuid.NewString(), f.parent.String(), "wallets", items); !errors.Is(err, mysql.ErrTenantScope) {
		t.Fatal("foreign tenant", err)
	}
	items[0].Fields["amount"] = "invalid"
	if _, err := f.preparer.PrepareRequest(ctx, f.tenant.String(), f.parent.String(), "wallets", items); err == nil {
		t.Fatal("invalid payload accepted")
	}
	items[0].Fields["amount"] = 10
	if _, err := f.db.ExecContext(ctx, "UPDATE "+f.schema+".data_model_physical_tables SET write_state='closed'"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.preparer.PrepareRequest(ctx, f.tenant.String(), "", "wallets", items); err == nil {
		t.Fatal("closed model allowed without scenario")
	}
	if _, err := f.db.ExecContext(ctx, "UPDATE "+f.schema+".data_model_physical_tables SET write_state='open',published_version=?", mysqlstore.UUID(uuid.New())); err != nil {
		t.Fatal(err)
	}
	if _, err := f.preparer.PrepareRequest(ctx, f.tenant.String(), f.parent.String(), "wallets", items); err == nil {
		t.Fatal("stale published version allowed")
	}
	f.revoked.Store(true)
	if _, err := f.preparer.PrepareRequest(ctx, f.tenant.String(), f.parent.String(), "wallets", items); !errors.Is(err, mysql.ErrDecisionAuthorizationDenied) {
		t.Fatal("revoked request allowed", err)
	}
}

func TestMySQLScheduledPreparationPinsOldVersionAndAtomicPreparedRollback(t *testing.T) {
	f := setupPreparation(t)
	ctx := context.Background()
	lease := f.running(t, []service.DecisionEvaluationRequest{{ObjectID: " exact ", ObjectType: "wallets", Fields: map[string]any{"amount": 10}}}, 0)
	// With a one-connection pool, a model provider which publishes a new live
	// version would block if preparation retained its SQL transaction over IO.
	f.db.SetMaxOpenConns(1)
	f.provider.onRead = func() { f.provider.onRead = nil; f.publish(t, 50) }
	prepared, err := f.preparer.PrepareScheduled(ctx, lease.Identity)
	if err != nil || prepared.IterationID() != f.old.String() || prepared.Len() != 1 {
		t.Fatal("redirected pinned evaluation", prepared, err)
	}
	if f.count(t, "decision_engine_decisions") != 0 || f.count(t, "decision_engine_effect_job_receipts") != 0 {
		t.Fatal("prepared outputs committed early")
	}
	rollback := errors.New("after all prepared writes")
	err = f.unit.CompleteScheduled(ctx, lease, func(s ports.DecisionMutationStore) error {
		if _, err := prepared.PersistAll(ctx, s); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) || f.count(t, "decision_engine_decisions") != 0 || f.count(t, "decision_engine_outbox_events") != 0 {
		t.Fatal("prepared batch rollback", err)
	}
	prepared, err = f.preparer.PrepareScheduled(ctx, lease.Identity)
	if err != nil {
		t.Fatal(err)
	}
	var results []service.DecisionEvaluationResult
	if err := f.unit.CompleteScheduled(ctx, lease, func(s ports.DecisionMutationStore) error {
		var err error
		results, err = prepared.PersistAll(ctx, s)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Decision == nil || results[0].Decision.Score != 15 || results[0].Decision.ScenarioIterationID != f.old.String() || results[0].Decision.ObjectID != " exact " {
		t.Fatal("wrong pinned result", results)
	}
	if f.count(t, "decision_engine_effect_job_receipts") != 1 || f.count(t, "decision_engine_rule_executions") != 1 {
		t.Fatal("required evidence/job missing")
	}
	if _, err := f.preparer.PrepareScheduled(ctx, lease.Identity); !errors.Is(err, mysql.ErrExecutionState) {
		t.Fatal("completed owner prepared again", err)
	}
}

func TestMySQLScheduledCandidatePreparationBoundsAndEmptyBatch(t *testing.T) {
	f := setupPreparation(t)
	ctx := context.Background()
	empty := f.running(t, nil, 2)
	prepared, err := f.preparer.PrepareScheduled(ctx, empty.Identity)
	if err != nil || prepared.Len() != 0 {
		t.Fatal("empty candidate set", prepared, err)
	}
	for i := 0; i < 3; i++ {
		physical, _ := mysqlstore.Identifier(f.physical)
		if _, err := f.db.ExecContext(ctx, "INSERT INTO "+f.schema+"."+physical+" VALUES(?,?,?,?)", fmt.Sprintf("record-%d", i), i, clock{}.Now(), mysqlstore.UUID(uuid.New())); err != nil {
			t.Fatal(err)
		}
	}
	prepared, err = f.preparer.PrepareScheduled(ctx, empty.Identity)
	if err != nil || prepared.Len() != 2 {
		t.Fatal("candidate bound/stored validation", prepared.Len(), err)
	}
	tooMany := f.running(t, nil, 11)
	if _, err := f.preparer.PrepareScheduled(ctx, tooMany.Identity); err == nil {
		t.Fatal("candidate overflow silently clamped")
	}
	foreign := empty.Identity
	foreign.TenantID = uuid.New()
	if _, err := f.preparer.PrepareScheduled(ctx, foreign); !errors.Is(err, mysql.ErrTenantScope) {
		t.Fatal("foreign worker scope", err)
	}
}
