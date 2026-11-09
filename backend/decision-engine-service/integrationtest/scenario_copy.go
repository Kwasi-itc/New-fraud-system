package integrationtest

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/postgres"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/testsupport"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
	"strings"
)

func (p *Preparation) VerifyScenarioCopyParity() {
	t := p.t
	t.Helper()
	ctx := context.Background()
	check := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	owner, e := mysql.NewAuthoringStore(p.db, p.database, p.tenant, 500, 2<<20, p.authorize)
	check(e)
	config, e := mysql.NewConfigurationStore(p.db, p.database, p.tenant, 500, 2<<20, p.authorize)
	check(e)
	id := p.path[strings.LastIndex(p.path, "/")+1:]
	var parent scenario.Scenario
	var iterations []scenario.Iteration
	var rules []scenario.Rule
	check(owner.ReadSnapshot(ctx, func(r mysql.AuthoringRepositories) error {
		var e error
		parent, e = r.Scenarios.GetByID(ctx, p.tenant.String(), id)
		if e != nil {
			return e
		}
		iterations, e = r.Iterations.ListByScenario(ctx, p.tenant.String(), id)
		if e != nil {
			return e
		}
		for _, it := range iterations {
			v, e := r.Rules.ListByIteration(ctx, p.tenant.String(), id, it.ID)
			if e != nil {
				return e
			}
			rules = append(rules, v...)
		}
		return nil
	}))
	if len(rules) != 1 {
		t.Fatal("unexpected source rules", len(rules))
	}
	at := Clock{}.Now()
	graphID := uuid.NewString()
	params, _ := json.Marshal(map[string][]string{"rule_ids": {rules[0].ID}})
	check(config.Mutate(ctx, func(r mysql.ConfigurationRepositories) error {
		if _, e := r.WorkflowRules.Create(ctx, workflow.Rule{ID: graphID, TenantID: p.tenant.String(), ScenarioID: id, Name: "Rule reference", Priority: 10, CreatedAt: at, UpdatedAt: at}); e != nil {
			return e
		}
		if _, e := r.WorkflowConditions.Create(ctx, workflow.Condition{ID: uuid.NewString(), TenantID: p.tenant.String(), RuleID: graphID, Function: workflow.ConditionRuleHit, Params: params, CreatedAt: at, UpdatedAt: at}); e != nil {
			return e
		}
		_, e := r.WorkflowActions.Create(ctx, workflow.Action{ID: uuid.NewString(), TenantID: p.tenant.String(), RuleID: graphID, ActionType: workflow.ActionTypeDoNothing, ActionConfig: []byte(`{}`), CreatedAt: at, UpdatedAt: at})
		return e
	}))
	var graphs []workflow.StructuredRule
	check(config.ReadSnapshot(ctx, func(r mysql.ConfigurationRepositories) error {
		var e error
		graphs, e = r.WorkflowRules.ListStructured(ctx, p.tenant.String(), id)
		return e
	}))
	pg := testsupport.SharedPostgresOwners(t)
	tx := postgres.NewTransactionManager(pg)
	_, e = pg.Exec(ctx, "INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'Copy','copy','active',now(),now())", p.tenant)
	check(e)
	check(tx.Run(ctx, func(r ports.MutationStore) error {
		original := parent
		original.LiveIterationID = nil
		if _, e := r.Scenarios().Create(ctx, original); e != nil {
			return e
		}
		for _, it := range iterations {
			if _, e := r.Iterations().Create(ctx, it); e != nil {
				return e
			}
		}
		for _, rule := range rules {
			if _, e := r.Rules().Create(ctx, rule); e != nil {
				return e
			}
		}
		for _, graph := range graphs {
			if _, e := r.WorkflowRules().Create(ctx, graph.Rule); e != nil {
				return e
			}
			for _, c := range graph.Conditions {
				if _, e := r.WorkflowConditions().Create(ctx, c); e != nil {
					return e
				}
			}
			for _, a := range graph.Actions {
				if _, e := r.WorkflowActions().Create(ctx, a); e != nil {
					return e
				}
			}
		}
		return r.Scenarios().SetLiveIterationID(ctx, p.tenant.String(), id, parent.LiveIterationID)
	}))
	source := service.NewScenarioService(tx, IDs{}, Clock{}, nil, postgres.NewScenarioRepository(pg), postgres.NewScenarioIterationRepository(pg), postgres.NewRuleRepository(pg), postgres.NewWorkflowRuleRepository(pg), postgres.NewWorkflowConditionRepository(pg), postgres.NewWorkflowActionRepository(pg))
	copyTarget := func() (scenario.Scenario, error) {
		return owner.CopyScenarioWithPreparation(ctx, mysql.ScenarioCopyInput{SourceID: uuid.MustParse(id), DestinationID: uuid.New(), Name: "Parity copy"}, func(ctx context.Context, snapshot mysql.ScenarioCopySnapshot) (mysql.ScenarioCopyGuard, mysql.PublicationGuard, error) {
			model, e := p.reader.ResolveAuthorizedPreparationModel(ctx)
			if e != nil {
				return nil, nil, e
			}
			permissions, e := owner.ScenarioCopyPermissions(&p.session, snapshot)
			if e != nil {
				return nil, nil, e
			}
			guard, e := p.reader.ScenarioCopyRegistryGuard(ctx, snapshot, model, permissions)
			if e != nil {
				return nil, nil, e
			}
			publish, e := p.reader.RegistryAggregatePublicationGuard(model, func(_ context.Context, _ *sql.Tx, s mysql.PublicationSnapshot) error {
				pin, e := service.NewPinnedScenarioSnapshot(s.Scenario, s.Iteration, s.Rules, service.ExecutionPreparationLimits{MaxItems: 500, MaxBytes: 2 << 20})
				if e != nil {
					return e
				}
				return service.ValidatePinnedModel(pin, model)
			})
			return guard, publish, e
		})
	}
	left, e := source.Copy(ctx, p.tenant.String(), id, "Parity copy")
	check(e)
	right, e := copyTarget()
	check(e)
	if left.Name != right.Name || left.TriggerObjectType != right.TriggerObjectType || left.LiveIterationID == nil || right.LiveIterationID == nil {
		t.Fatal("scenario copy mismatch")
	}
	leftRules, e := postgres.NewRuleRepository(pg).ListByIteration(ctx, p.tenant.String(), left.ID, *left.LiveIterationID)
	check(e)
	leftGraphs, e := postgres.NewWorkflowRuleRepository(pg).ListStructured(ctx, p.tenant.String(), left.ID)
	check(e)
	var rightRules []scenario.Rule
	var rightGraphs []workflow.StructuredRule
	check(owner.ReadSnapshot(ctx, func(r mysql.AuthoringRepositories) error {
		var e error
		rightRules, e = r.Rules.ListByIteration(ctx, p.tenant.String(), right.ID, *right.LiveIterationID)
		return e
	}))
	check(config.ReadSnapshot(ctx, func(r mysql.ConfigurationRepositories) error {
		var e error
		rightGraphs, e = r.WorkflowRules.ListStructured(ctx, p.tenant.String(), right.ID)
		return e
	}))
	for i, set := range [][]scenario.Rule{leftRules, rightRules} {
		if len(set) != 1 || set[0].ID == rules[0].ID || set[0].StableRuleID == rules[0].StableRuleID || set[0].ScoreModifier != rules[0].ScoreModifier {
			t.Fatal("rule copy mismatch", i, set)
		}
		gs := leftGraphs
		if i == 1 {
			gs = rightGraphs
		}
		if len(gs) != len(graphs) {
			t.Fatal("graph omitted")
		}
		found := false
		for _, g := range gs {
			for _, c := range g.Conditions {
				if c.Function == workflow.ConditionRuleHit {
					var refs map[string][]string
					check(json.Unmarshal(c.Params, &refs))
					if len(refs["rule_ids"]) != 1 || refs["rule_ids"][0] != set[0].ID {
						t.Fatal("copied reference not remapped", refs)
					}
					found = true
				}
			}
		}
		if !found {
			t.Fatal("reference condition omitted")
		}
	}
	// Required graph-write failure must roll back the newly created parent too.
	var pgBefore int
	check(pg.QueryRow(ctx, "SELECT count(*) FROM core.scenarios WHERE tenant_id=$1", p.tenant).Scan(&pgBefore))
	_, e = pg.Exec(ctx, "ALTER TABLE core.workflow_actions ADD CONSTRAINT copy_reject CHECK(id IS NULL) NOT VALID")
	check(e)
	if _, e = source.Copy(ctx, p.tenant.String(), id, "Must rollback"); e == nil {
		t.Fatal("source copy ignored graph failure")
	}
	_, e = pg.Exec(ctx, "ALTER TABLE core.workflow_actions DROP CONSTRAINT copy_reject")
	check(e)
	var pgAfter int
	check(pg.QueryRow(ctx, "SELECT count(*) FROM core.scenarios WHERE tenant_id=$1", p.tenant).Scan(&pgAfter))
	if pgBefore != pgAfter {
		t.Fatal("partial source parent")
	}
	schema, _ := mysqlstore.Identifier(p.database)
	var myBefore, myAfter int
	check(p.db.QueryRowContext(ctx, "SELECT count(*) FROM "+schema+".decision_engine_scenarios WHERE tenant_id=?", mysqlstore.UUID(p.tenant)).Scan(&myBefore))
	_, e = p.db.ExecContext(ctx, "CREATE TRIGGER "+schema+".fixture_graph_fail BEFORE INSERT ON "+schema+".decision_engine_workflow_actions FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='graph fail'")
	check(e)
	if _, e = copyTarget(); e == nil {
		t.Fatal("target copy ignored graph failure")
	}
	_, e = p.db.ExecContext(ctx, "DROP TRIGGER "+schema+".fixture_graph_fail")
	check(e)
	check(p.db.QueryRowContext(ctx, "SELECT count(*) FROM "+schema+".decision_engine_scenarios WHERE tenant_id=?", mysqlstore.UUID(p.tenant)).Scan(&myAfter))
	if myBefore != myAfter {
		t.Fatal("partial target parent")
	}
}
