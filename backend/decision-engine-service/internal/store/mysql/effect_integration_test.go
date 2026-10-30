package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type effectFixture struct {
	platformFixture
	store          *EffectStore
	tenant, parent uuid.UUID
	iteration      scenario.Iteration
	wf             workflow.Definition
	screen         screening.Config
	score          scoring.Config
}

func effectsFixture(t *testing.T, queued ...bool) effectFixture {
	t.Helper()
	ctx := context.Background()
	withQueues := len(queued) > 0 && queued[0]
	withMaintenance := len(queued) > 1 && queued[1]
	withCancellation := len(queued) > 2 && queued[2]
	withDispatch := len(queued) > 3 && queued[3]
	f := scenarioFoundationDB(t, true, true, true, true, true, true, true, true, withQueues, withMaintenance, withCancellation, withDispatch)
	tenant, parent := uuid.New(), uuid.New()
	if err := scenarioInsert(ctx, f.db, f.database, tenant, parent, "Effect owner"); err != nil {
		t.Fatal(err)
	}
	a := authoringStore(t, f, tenant, 500, 2<<20)
	iteration := createAuthoringDraft(t, a, tenant, parent)
	c, err := NewConfigurationStore(f.db, f.database, tenant, 500, 2<<20, allowAuthoringFixture)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	wf := workflow.Definition{ID: uuid.NewString(), TenantID: tenant.String(), ScenarioID: parent.String(), Name: "Emit", ActionType: workflow.ActionTypeEmitEvent, ActionConfig: []byte(`{"url":"https://example.test/event"}`), AllowedOutcomes: []string{"review"}, Active: true, CreatedAt: at, UpdatedAt: at}
	screen := screening.Config{ID: uuid.NewString(), TenantID: tenant.String(), ScenarioID: parent.String(), Name: "Screen", Provider: "fixture", ConfigJSON: []byte(`{}`), AllowedOutcomes: []string{"review"}, Active: true, CreatedAt: at, UpdatedAt: at}
	score := scoring.Config{ID: uuid.NewString(), TenantID: tenant.String(), ScenarioID: parent.String(), Name: "Score", RulesetRef: "fixture", ConfigJSON: []byte(`{}`), AllowedOutcomes: []string{"review"}, Active: true, CreatedAt: at, UpdatedAt: at}
	if err := c.Mutate(ctx, func(r ConfigurationRepositories) error {
		if _, err := r.Workflows.Create(ctx, wf); err != nil {
			return err
		}
		if _, err := r.ScreeningConfigs.Create(ctx, screen); err != nil {
			return err
		}
		_, err := r.ScoringConfigs.Create(ctx, score)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	source, err := NewScreeningSourceContract(f.database, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewEffectStore(f.db, f.database, tenant, 500, 2<<20, allowPlatformFixture, &source)
	if err != nil {
		t.Fatal(err)
	}
	return effectFixture{f, s, tenant, parent, iteration, wf, screen, score}
}
func (f effectFixture) screeningInput(d decision.Decision) screening.Execution {
	return screening.Execution{ID: uuid.NewString(), TenantID: f.tenant.String(), ConfigID: f.screen.ID, DecisionID: d.ID, ScenarioID: f.parent.String(), Status: screening.ExecutionStatusPending, RequestJSON: []byte(`{"n":9007199254740993,"optional":null}`), CreatedAt: d.CreatedAt, UpdatedAt: d.CreatedAt}
}

func TestMySQLEffectsBatchRelationshipsAndAtomicRollback(t *testing.T) {
	f := effectsFixture(t)
	ctx := context.Background()
	d := decisionInput(f.tenant, f.parent, f.iteration.ID)
	screens := make([]screening.Execution, 120)
	scores := make([]scoring.Request, 120)
	work := make([]workflow.Execution, 120)
	events := make([]integration.OutboxEvent, 120)
	for i := range screens {
		screens[i] = f.screeningInput(d)
		v := screens[i]
		scores[i] = scoring.Request{ID: uuid.NewString(), TenantID: v.TenantID, ConfigID: f.score.ID, DecisionID: v.DecisionID, ScenarioID: v.ScenarioID, Status: scoring.RequestStatusPending, RequestJSON: v.RequestJSON, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
		wfID := f.wf.ID
		work[i] = workflow.Execution{ID: uuid.NewString(), TenantID: v.TenantID, WorkflowID: &wfID, DecisionID: d.ID, ScenarioID: v.ScenarioID, ActionType: f.wf.ActionType, ActionConfig: f.wf.ActionConfig, Status: workflow.ExecutionStatusPendingDispatch, CreatedAt: d.CreatedAt}
		events[i] = integration.OutboxEvent{ID: uuid.NewString(), TenantID: v.TenantID, AggregateType: "decision", AggregateID: d.ID, EventType: "decision.created", Payload: []byte(`{"n":9007199254740993}`), Status: integration.OutboxStatusPending, CreatedAt: d.CreatedAt}
	}
	err := f.store.Mutate(ctx, func(r EffectRepositories) error {
		if _, err := r.Decisions.Create(ctx, d); err != nil {
			return err
		}
		a, err := r.ScreeningExecutions.CreateMany(ctx, screens)
		if err != nil {
			return err
		}
		if len(a) != 120 || a[0].ID != screens[0].ID || a[119].ID != screens[119].ID || string(a[0].ResponseJSON) != "{}" {
			t.Fatal("screening readback", a)
		}
		b, err := r.ScoringRequests.CreateMany(ctx, scores)
		if err != nil {
			return err
		}
		if len(b) != 120 || b[0].ID != scores[0].ID {
			t.Fatal("scoring readback")
		}
		c, err := r.WorkflowExecutions.CreateMany(ctx, work)
		if err != nil {
			return err
		}
		if len(c) != 120 || c[119].ID != work[119].ID {
			t.Fatal("workflow readback")
		}
		e, err := r.OutboxEvents.CreateMany(ctx, events)
		if err != nil {
			return err
		}
		if len(e) != 120 || e[0].ID != events[0].ID {
			t.Fatal("outbox readback")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.Mutate(ctx, func(r EffectRepositories) error {
		v, err := r.ScreeningExecutions.GetByID(ctx, f.tenant.String(), screens[0].ID)
		if err != nil {
			return err
		}
		v.ResponseJSON = []byte(`{"ok":true}`)
		v.Status = screening.ExecutionStatusSent
		v.SentAt = &v.CreatedAt
		v.ProviderReference = "exact reference "
		if _, err := r.ScreeningExecutions.Update(ctx, v); err != nil {
			return err
		}
		if err := r.ScoringRequests.UpdateStatus(ctx, scores[0].ID, scoring.RequestStatusCompleted); err != nil {
			return err
		}
		if err := r.WorkflowExecutions.UpdateStatus(ctx, work[0].ID, workflow.ExecutionStatusDispatched); err != nil {
			return err
		}
		return r.OutboxEvents.UpdateStatus(ctx, events[0].ID, integration.OutboxStatusSent)
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.ReadSnapshot(ctx, func(r EffectRepositories) error {
		if _, err := r.ScreeningExecutions.GetByID(ctx, uuid.NewString(), screens[0].ID); !errors.Is(err, ErrTenantScope) {
			t.Fatal("tenant leak", err)
		}
		if _, err := r.ScoringRequests.ListByStatus(ctx, scoring.RequestStatusPending, 1); !errors.Is(err, ErrSystemScopeRequired) {
			t.Fatal("implicit system access", err)
		}
		page, err := r.OutboxEvents.ListByTenant(ctx, f.tenant.String(), 2)
		if err != nil {
			return err
		}
		if len(page) != 2 {
			t.Fatal("bounded event page")
		}
		v, err := r.ScoringRequests.GetByID(ctx, f.tenant.String(), scores[0].ID)
		if err != nil {
			return err
		}
		if v.Status != scoring.RequestStatusCompleted || v.CompletedAt == nil {
			t.Fatal("status timestamp")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Cross-scenario configuration in a later chunk must roll back the new
	// decision, earlier effects and related platform writes in this transaction.
	other := uuid.New()
	if err := scenarioInsert(ctx, f.db, f.database, f.tenant, other, "Other"); err != nil {
		t.Fatal(err)
	}
	config := f.screen
	config.ID = uuid.NewString()
	config.ScenarioID = other.String()
	c, err := NewConfigurationStore(f.db, f.database, f.tenant, 500, 2<<20, allowAuthoringFixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Mutate(ctx, func(r ConfigurationRepositories) error { _, err := r.ScreeningConfigs.Create(ctx, config); return err }); err != nil {
		t.Fatal(err)
	}
	bad := decisionInput(f.tenant, f.parent, f.iteration.ID)
	badScreens := make([]screening.Execution, 120)
	for i := range badScreens {
		badScreens[i] = f.screeningInput(bad)
	}
	badScreens[110].ConfigID = config.ID
	tag := platform.RecordTag{ID: uuid.NewString(), TenantID: f.tenant.String(), ObjectType: "wallets", ObjectID: bad.ObjectID, Tag: "rollback", CreatedAt: bad.CreatedAt}
	if err := f.store.Mutate(ctx, func(r EffectRepositories) error {
		if _, err := r.Decisions.Create(ctx, bad); err != nil {
			return err
		}
		if _, err := r.RecordTags.Create(ctx, tag); err != nil {
			return err
		}
		_, err := r.ScreeningExecutions.CreateMany(ctx, badScreens)
		return err
	}); err == nil {
		t.Fatal("cross-scenario configuration accepted")
	}
	schema, _ := mysqlstore.Identifier(f.database)
	for _, q := range []string{"SELECT COUNT(*) FROM " + schema + ".decision_engine_decisions WHERE id=?", "SELECT COUNT(*) FROM " + schema + ".decision_engine_screening_executions WHERE decision_id=?"} {
		var count int
		if err := f.db.QueryRowContext(ctx, q, mysqlstore.UUID(uuid.MustParse(bad.ID))).Scan(&count); err != nil || count != 0 {
			t.Fatal("partial effects", count, err)
		}
	}
	if err := f.store.ReadSnapshot(ctx, func(r EffectRepositories) error {
		ok, err := r.RecordTags.HasTag(ctx, f.tenant.String(), tag.ObjectType, tag.ObjectID, tag.Tag)
		if ok {
			t.Fatal("platform effect survived rollback")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// Tenant-less status APIs remain tenant-bound by their store, never global.
	foreign, err := NewEffectStore(f.db, f.database, uuid.New(), 500, 2<<20, allowPlatformFixture, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := foreign.Mutate(ctx, func(r EffectRepositories) error {
		return r.WorkflowExecutions.UpdateStatus(ctx, work[0].ID, workflow.ExecutionStatusDispatched)
	}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("foreign mutation", err)
	}
}

func TestMySQLScreeningProjectionMonotonicAuthority(t *testing.T) {
	f := effectsFixture(t)
	ctx := context.Background()
	d := decisionInput(f.tenant, f.parent, f.iteration.ID)
	item := f.screeningInput(d)
	if err := f.store.Mutate(ctx, func(r EffectRepositories) error {
		if _, err := r.Decisions.Create(ctx, d); err != nil {
			return err
		}
		_, err := r.ScreeningExecutions.CreateMany(ctx, []screening.Execution{item})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	source := uuid.New()
	reconcile := func() (screening.Execution, error) {
		var result screening.Execution
		err := f.store.Mutate(ctx, func(r EffectRepositories) error {
			var err error
			result, err = r.ScreeningExecutions.ReconcileSource(ctx, f.tenant.String(), source.String(), d.ID, f.screen.ID)
			return err
		})
		return result, err
	}
	if _, err := reconcile(); !errors.Is(err, ErrOwnerContractUnavailable) {
		t.Fatal("missing owner provider", err)
	}
	schema, _ := mysqlstore.Identifier(f.database)
	if _, err := f.db.ExecContext(ctx, "CREATE TABLE "+schema+".fixture_projection_source(id BINARY(16),tenant_id BINARY(16),decision_id BINARY(16),scenario_id BINARY(16),config_id BINARY(16),execution_id BINARY(16),projection_version BIGINT,status VARCHAR(32),updated_at DATETIME(6),sent_at DATETIME(6) NULL,completed_at DATETIME(6) NULL,failed_at DATETIME(6) NULL,response_json JSON,provider_reference LONGTEXT,last_error LONGTEXT,PRIMARY KEY(tenant_id,id)) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(ctx, "CREATE ALGORITHM=MERGE VIEW "+schema+".screening_decision_source_v1 AS SELECT * FROM "+schema+".fixture_projection_source"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(ctx, "INSERT INTO "+schema+".fixture_projection_source VALUES(?,?,?,?,?,?,3,'no_hit',?,NULL,?,NULL,JSON_OBJECT('match_count',0,'n',9007199254740993),'provider ref','')", mysqlstore.UUID(source), mysqlstore.UUID(f.tenant), mysqlstore.UUID(uuid.MustParse(d.ID)), mysqlstore.UUID(f.parent), mysqlstore.UUID(uuid.MustParse(f.screen.ID)), mysqlstore.UUID(uuid.MustParse(item.ID)), mysqlstore.DateTime{Time: d.CreatedAt}, mysqlstore.DateTime{Time: d.CreatedAt}); err != nil {
		t.Fatal(err)
	}
	result, err := reconcile()
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != screening.ExecutionStatusCompleted || result.CompletedAt == nil {
		t.Fatal(result)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(result.ResponseJSON, &body); err != nil || string(body["n"]) != "9007199254740993" {
		t.Fatal("projection number fidelity", err, string(result.ResponseJSON))
	}
	// Older/same delivery never overwrites newer evidence, even with a changed
	// source payload. A higher owner version can change the projection.
	if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".fixture_projection_source SET status='failed',projection_version=2,last_error='older'"); err != nil {
		t.Fatal(err)
	}
	older, err := reconcile()
	if err != nil || older.Status != screening.ExecutionStatusCompleted || older.LastError != "" {
		t.Fatal("source regression", older, err)
	}
	if err := f.store.Mutate(ctx, func(r EffectRepositories) error {
		return r.ScreeningExecutions.UpdateStatus(ctx, item.ID, screening.ExecutionStatusFailed)
	}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("legacy dispatcher overwrote source", err)
	}
	if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".decision_engine_screening_executions SET status='failed' WHERE id=?", mysqlstore.UUID(uuid.MustParse(item.ID))); err == nil {
		t.Fatal("unversioned source mutation accepted")
	}
	if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".fixture_projection_source SET projection_version=4"); err != nil {
		t.Fatal(err)
	}
	newer, err := reconcile()
	if err != nil || newer.Status != screening.ExecutionStatusFailed || newer.FailedAt == nil || newer.CompletedAt == nil {
		t.Fatal("new source version", newer, err)
	}
	if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".fixture_projection_source SET scenario_id=?", mysqlstore.UUID(uuid.New())); err != nil {
		t.Fatal(err)
	}
	if _, err := reconcile(); !errors.Is(err, ErrOwnerRelationshipConflict) {
		t.Fatal("mismatched source scenario", err)
	}
}
