package integrationtest

import (
	"context"
	"database/sql"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqlexecution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
	"strings"
)

// VerifyPinnedModelChange closes the actual owner publication gate after real
// evaluation. A completed evaluation is immutable: local persistence retains
// its pinned scenario and evidence; new preparations must reject the closed gate.
func (p *Preparation) VerifyPinnedModelChange() {
	t := p.t
	t.Helper()
	ctx := context.Background()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	authority := func(ctx context.Context, tx *sql.Tx, tenant uuid.UUID, write bool) error {
		action := mysql.AuthoringRead
		if write {
			action = mysql.AuthoringWrite
		}
		return p.authorize(ctx, tx, tenant, action)
	}
	config, err := mysql.NewConfigurationStore(p.db, p.database, p.tenant, 500, 2<<20, p.authorize)
	check(err)
	history, err := mysql.NewDecisionStore(p.db, p.database, p.tenant, 500, 2<<20, authority)
	check(err)
	platform, err := mysql.NewPlatformStore(p.db, p.database, p.tenant, 500, 2<<20, authority)
	check(err)
	lists, err := mysql.NewCustomListStore(p.db, p.database, p.tenant, 500, 2<<20, p.authorize)
	check(err)
	reads, err := mysql.NewEvaluationReaders(p.executions, config, history, platform, lists)
	check(err)
	limits := service.ExecutionPreparationLimits{MaxItems: 100, MaxBytes: 2 << 20}
	evaluator := service.NewDecisionService(nil, IDs{}, Clock{}, nil, nil, nil, nil, p.reader, nil, nil, reads.Workflows(), reads.WorkflowRules(), nil, nil, nil, reads.Snoozes(), nil, reads.CustomLists(), reads.RecordTags(), reads.RiskSnapshots(), reads.IPFlags(), reads.ScreeningConfigs(), nil, reads.ScoringConfigs(), nil, false, "enabled", []string{"count", "count_distinct", "sum", "avg", "min", "max"}, 1, 1, 1, nil)
	preparer, err := mysqlexecution.NewPreparer(p.executions, p.reader, evaluator, limits)
	check(err)
	model, err := p.reader.ResolveExecutionModel(ctx)
	check(err)
	var object string
	for name, table := range model.Tables {
		if table.ID == p.table.String() {
			object = name
		}
	}
	if object == "" {
		t.Fatal("published table missing")
	}
	parent := p.path[strings.LastIndex(p.path, "/")+1:]
	request := []service.DecisionEvaluationRequest{{ObjectID: "pinned-model-change", ObjectType: object, Fields: map[string]any{"score": float64(7)}}}
	prepared, err := preparer.PrepareRequest(ctx, p.tenant.String(), parent, object, request)
	check(err)
	pin, ok := prepared.PinnedScenario()
	if !ok {
		t.Fatal("scenario not pinned")
	}
	plans, err := evaluator.PreparePinnedBatch(ctx, pin, prepared.Batch())
	check(err)
	if len(plans) != 1 {
		t.Fatal("wrong plan count")
	}
	schema, _ := mysqlstore.Identifier(p.database)
	_, err = p.db.ExecContext(ctx, "UPDATE "+schema+".data_model_physical_tables SET write_state='closed' WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(p.tenant), mysqlstore.UUID(p.table))
	check(err)
	defer func() {
		_, e := p.db.ExecContext(ctx, "UPDATE "+schema+".data_model_physical_tables SET write_state='open' WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(p.tenant), mysqlstore.UUID(p.table))
		if e != nil {
			t.Error(e)
		}
	}()
	if _, err = preparer.PrepareRequest(ctx, p.tenant.String(), parent, object, request); err == nil {
		t.Fatal("new preparation accepted closed publication")
	}
	queues := map[ports.JobKind]mysql.EffectQueuePolicy{}
	for _, kind := range []ports.JobKind{ports.JobWorkflow, ports.JobScreening, ports.JobScoring, ports.JobOutbox} {
		queues[kind] = mysql.EffectQueuePolicy{Name: string(kind), MaxAttempts: 3}
	}
	effects, err := mysql.NewQueuedEffectStore(p.db, p.database, p.tenant, 500, 2<<20, authority, nil, queues)
	check(err)
	unit, err := mysql.NewDecisionUnitOfWorkStore(effects, p.executions)
	check(err)
	var result service.DecisionEvaluationResult
	err = unit.RunDecisionMutation(ctx, func(store ports.DecisionMutationStore) error {
		var e error
		result, e = plans[0].Persist(ctx, store)
		return e
	})
	check(err)
	if result.Decision == nil || result.Decision.ScenarioIterationID != p.iteration || len(result.RuleExecutions) != 1 {
		t.Fatalf("pinned evidence lost: %+v", result)
	}
	check(history.ReadSnapshot(ctx, func(r mysql.DecisionRepositories) error {
		saved, e := r.Decisions.GetByID(ctx, p.tenant.String(), result.Decision.ID)
		if e != nil {
			return e
		}
		evidence, e := r.RuleExecutions.ListByDecision(ctx, p.tenant.String(), saved.ID)
		if e != nil {
			return e
		}
		if saved.ScenarioIterationID != p.iteration || len(evidence) != 1 {
			t.Fatal("persisted pin/evidence mismatch")
		}
		return nil
	}))
}
