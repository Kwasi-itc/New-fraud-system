package integrationtest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqlexecution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// The browser rule was saved/copied with actual session, model and inbox owners.
// Publish a decision iteration on that destination and evaluate its saved graph;
// this proves local business effects, not external provider delivery.
func (p *Preparation) verifyBrowserDecision(parent uuid.UUID, object string, copied workflow.CaseRule) {
	t := p.t
	t.Helper()
	ctx := context.Background()
	check := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	path := "/v1/tenants/" + p.tenant.String() + "/scenarios/" + parent.String()
	iteration := p.id(p.request("POST", path+"/iterations", "", 201), "iteration")
	ip := path + "/iterations/" + iteration
	p.request("PUT", ip, `{"trigger_formula":{"constant":true},"score_review_threshold":1,"score_block_and_review_threshold":10,"score_decline_threshold":20}`, 200)
	p.request("POST", ip+"/rules", `{"name":"Browser journey","formula":{"constant":true},"score_modifier":1,"rule_group":"journey","stable_rule_id":"browser-journey"}`, 201)
	p.request("POST", ip+"/commit", "{}", 200)
	p.request("POST", path+"/publications", fmt.Sprintf(`{"action":"publish","iteration_id":%q}`, iteration), 200)
	authority := func(ctx context.Context, tx *sql.Tx, tenant uuid.UUID, write bool) error {
		action := mysql.AuthoringRead
		if write {
			action = mysql.AuthoringWrite
		}
		return p.authorize(ctx, tx, tenant, action)
	}
	config, e := mysql.NewConfigurationStore(p.db, p.database, p.tenant, 500, 2<<20, p.authorize)
	check(e)
	history, e := mysql.NewDecisionStore(p.db, p.database, p.tenant, 500, 2<<20, authority)
	check(e)
	platform, e := mysql.NewPlatformStore(p.db, p.database, p.tenant, 500, 2<<20, authority)
	check(e)
	lists, e := mysql.NewCustomListStore(p.db, p.database, p.tenant, 500, 2<<20, p.authorize)
	check(e)
	reads, e := mysql.NewEvaluationReaders(p.executions, config, history, platform, lists)
	check(e)
	evaluator := service.NewDecisionService(nil, IDs{}, Clock{}, nil, nil, nil, nil, p.reader, nil, nil, reads.Workflows(), reads.WorkflowRules(), nil, nil, nil, reads.Snoozes(), nil, reads.CustomLists(), reads.RecordTags(), reads.RiskSnapshots(), reads.IPFlags(), reads.ScreeningConfigs(), nil, reads.ScoringConfigs(), nil, false, "strict", []string{"count", "sum"}, 1, 1, 1, nil)
	preparer, e := mysqlexecution.NewPreparer(p.executions, p.reader, evaluator, service.ExecutionPreparationLimits{MaxItems: 500, MaxBytes: 2 << 20})
	check(e)
	prepared, e := preparer.PrepareRequest(ctx, p.tenant.String(), parent.String(), object, []service.DecisionEvaluationRequest{{ObjectID: "browser-journey", ObjectType: object, Fields: map[string]any{"score": 7}}})
	check(e)
	pin, ok := prepared.PinnedScenario()
	if !ok {
		t.Fatal("browser iteration not pinned")
	}
	plans, e := evaluator.PreparePinnedBatch(ctx, pin, prepared.Batch())
	check(e)
	if len(plans) != 1 {
		t.Fatal("browser plan missing")
	}
	queues := map[ports.JobKind]mysql.EffectQueuePolicy{}
	for _, kind := range []ports.JobKind{ports.JobWorkflow, ports.JobScreening, ports.JobScoring, ports.JobOutbox} {
		queues[kind] = mysql.EffectQueuePolicy{Name: string(kind), MaxAttempts: 3}
	}
	effects, e := mysql.NewQueuedEffectStore(p.db, p.database, p.tenant, 500, 2<<20, authority, nil, queues)
	check(e)
	unit, e := mysql.NewDecisionUnitOfWorkStore(effects, p.executions)
	check(e)
	var result service.DecisionEvaluationResult
	check(unit.RunDecisionMutation(ctx, func(store ports.DecisionMutationStore) error {
		var e error
		result, e = plans[0].Persist(ctx, store)
		return e
	}))
	if result.Decision == nil || result.Decision.ScenarioIterationID != iteration || result.Decision.Score != 1 || result.Decision.Outcome != "review" {
		t.Fatalf("browser decision mismatch: %+v", result)
	}
	check(history.ReadSnapshot(ctx, func(r mysql.DecisionRepositories) error {
		saved, e := r.Decisions.GetByID(ctx, p.tenant.String(), result.Decision.ID)
		if e != nil {
			return e
		}
		rules, e := r.RuleExecutions.ListByDecision(ctx, p.tenant.String(), saved.ID)
		if e != nil {
			return e
		}
		if len(rules) != 1 || rules[0].Outcome != "hit" || saved.ScenarioIterationID != iteration {
			t.Fatal("browser decision evidence mismatch")
		}
		return nil
	}))
	check(effects.ReadSnapshot(ctx, func(r mysql.EffectRepositories) error {
		rows, e := r.WorkflowExecutions.ListByDecision(ctx, p.tenant.String(), result.Decision.ID)
		if e != nil {
			return e
		}
		if len(rows) != 1 || rows[0].ActionType != copied.ActionType || rows[0].WorkflowRuleID == nil || *rows[0].WorkflowRuleID != copied.ID {
			t.Fatalf("copied browser graph not evaluated: %+v", rows)
		}
		var action map[string]any
		if e := json.Unmarshal(rows[0].ActionConfig, &action); e != nil {
			return e
		}
		if action["inbox_id"] != copied.InboxID || action["name"] != copied.Title {
			t.Fatalf("copied action changed: %+v", action)
		}
		return nil
	}))
	schema, _ := mysqlstore.Identifier(p.database)
	var audits int
	check(p.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".decision_engine_workflow_authoring_events WHERE tenant_id=? AND scenario_id=? AND rule_id=? AND actor_id=?", mysqlstore.UUID(p.tenant), mysqlstore.UUID(parent), mysqlstore.UUID(uuid.MustParse(copied.ID)), mysqlstore.UUID(p.session.ActorID)).Scan(&audits))
	if audits != 1 {
		t.Fatalf("copied browser audit lost or duplicated: %d", audits)
	}
}
