package integrationtest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/postgres"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/testsupport"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"reflect"
	"sort"
	"strings"
)

// VerifyAggregateParity seeds bounded records in the physically published owner
// table, mirrors its logical schema in the disposable source database and runs
// both actual decision services. The source fixture does not claim DDL upgrade
// parity; the target table/token were installed by the real physical owner.
func (p *Preparation) VerifyAggregateParity() {
	t := p.t
	t.Helper()
	ctx := context.Background()
	check := func(e error) {
		t.Helper()
		if e != nil {
			var local *ports.LocalTenantReadError
			if errors.As(e, &local) {
				t.Fatalf("%v: %v", e, local.Cause)
			}
			t.Fatal(e)
		}
	}
	schema, _ := mysqlstore.Identifier(p.database)
	var physical string
	check(p.db.QueryRowContext(ctx, "SELECT physical_name FROM "+schema+".data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(p.tenant), mysqlstore.UUID(p.table)).Scan(&physical))
	columns := map[string]string{}
	fieldIDs := map[string]mysqlstore.UUID{}
	rows, e := p.db.QueryContext(ctx, "SELECT f.name,f.id,m.physical_name FROM "+schema+".data_model_fields f JOIN "+schema+".data_model_physical_fields m ON m.tenant_id=f.tenant_id AND m.table_id=f.table_id AND m.field_id=f.id AND m.component='value' WHERE f.tenant_id=? AND f.table_id=?", mysqlstore.UUID(p.tenant), mysqlstore.UUID(p.table))
	check(e)
	for rows.Next() {
		var name, column string
		var id mysqlstore.UUID
		check(rows.Scan(&name, &id, &column))
		columns[name] = column
		fieldIDs[name] = id
	}
	check(rows.Err())
	check(rows.Close())
	// A self-reference supplies a real published-table navigation path. This is
	// privileged isolated fixture data, not a runtime cross-owner mutation API.
	_, e = p.db.ExecContext(ctx, "INSERT INTO "+schema+".data_model_links(id,tenant_id,name,parent_table_id,parent_field_id,child_table_id,child_field_id,created_at) VALUES(?,?,'self',?,?,?,?,SYSDATE(6))", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(p.tenant), mysqlstore.UUID(p.table), fieldIDs["object_id"], mysqlstore.UUID(p.table), fieldIDs["object_id"])
	check(e)
	model, e := p.reader.ResolveExecutionModel(ctx)
	check(e)
	var object string
	for name, table := range model.Tables {
		if table.ID == p.table.String() {
			object = name
		}
	}
	if object == "" || !model.Tables[object].Fields["score"].Nullable {
		t.Fatal("expected published nullable score")
	}
	pg := testsupport.SharedPostgresOwners(t)
	pgSchema := "tenant_" + strings.ReplaceAll(p.tenant.String(), "-", "")
	pgTable := pgx.Identifier{pgSchema, object}.Sanitize()
	_, e = pg.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{pgSchema}.Sanitize())
	check(e)
	_, e = pg.Exec(ctx, "CREATE TABLE "+pgTable+"(object_id TEXT PRIMARY KEY,updated_at TIMESTAMPTZ NOT NULL,score BIGINT NULL)")
	check(e)
	physicalSQL, _ := mysqlstore.Identifier(physical)
	quoted := func(name string) string { v, e := mysqlstore.Identifier(columns[name]); check(e); return v }
	for i, score := range []any{10, 20, 20, nil} {
		objectID := fmt.Sprintf("golden-%d", i)
		at := Clock{}.Now()
		_, e = p.db.ExecContext(ctx, "INSERT INTO "+schema+"."+physicalSQL+"(id,"+quoted("object_id")+","+quoted("updated_at")+","+quoted("score")+",record_object_key_id) VALUES(?,?,?,?,?)", mysqlstore.UUID(uuid.New()), objectID, at, score, mysqlstore.UUID(uuid.New()))
		check(e)
		_, e = pg.Exec(ctx, "INSERT INTO "+pgTable+" VALUES($1,$2,$3)", objectID, at, score)
		check(e)
	}
	targetReader := p.reader.WithModel(p.tenant.String(), model)
	sourceReader := postgres.NewTenantDataReader(pg, nil).WithModel(p.tenant.String(), model)
	// Build actual boolean rules for supported aggregation, empty results,
	// grouped filters, null semantics, and navigation. Every rule should match.
	constant := func(v any) map[string]any { return map[string]any{"constant": v} }
	fn := func(name string, children ...any) map[string]any {
		return map[string]any{"function": name, "children": children}
	}
	predicate := func(op string, value any) map[string]any {
		return map[string]any{"function": "Filter", "named_children": map[string]any{"tableName": constant(object), "fieldName": constant("score"), "operator": constant(op), "value": constant(value)}}
	}
	aggregate := func(kind string, filter any) map[string]any {
		named := map[string]any{"tableName": constant(object), "fieldName": constant("score"), "aggregator": constant(kind)}
		if filter != nil {
			named["filters"] = filter
		}
		return map[string]any{"function": "Aggregator", "named_children": named}
	}
	formulas := []any{}
	names := []string{}
	add := func(name string, expr any, want any) {
		names = append(names, name)
		if want == nil {
			formulas = append(formulas, fn("is_null", expr))
		} else {
			formulas = append(formulas, fn("eq", expr, constant(want)))
		}
	}
	for _, c := range []struct {
		kind  string
		want  any
		empty any
	}{{"COUNT", 3, 0}, {"COUNT_DISTINCT", 2, 0}, {"SUM", 50, 0}, {"AVG", float64(50) / 3, nil}, {"MIN", 10, nil}, {"MAX", 20, nil}} {
		add(c.kind, aggregate(c.kind, nil), c.want)
		add(c.kind+" empty", aggregate(c.kind, predicate(">", 100)), c.empty)
	}
	add("group and", aggregate("SUM", fn("and", predicate(">=", 10), predicate("<", 20))), 10)
	add("group or", aggregate("COUNT", fn("or", predicate("=", 10), predicate("=", 20))), 3)
	add("group not", aggregate("COUNT", fn("not", predicate("=", 20))), 1)
	add("null excluded", aggregate("COUNT", predicate("!=", 20)), 1)
	add("explicit null", aggregate("SUM", predicate("=", nil)), 0)
	add("related field", map[string]any{"function": "related_field", "named_children": map[string]any{"path": constant("self"), "field": constant("score")}}, 10)
	add("database access", map[string]any{"function": "DatabaseAccess", "named_children": map[string]any{"tableName": constant(object), "fieldName": constant("score"), "path": constant([]string{"self"})}}, 10)
	base := "/v1/tenants/" + p.tenant.String() + "/scenarios"
	parentID := p.id(p.request("POST", base, fmt.Sprintf(`{"name":"Aggregate golden","trigger_object_type":%q}`, object), 201), "scenario")
	parentPath := base + "/" + parentID
	iterationID := p.id(p.request("POST", parentPath+"/iterations", "", 201), "iteration")
	ip := parentPath + "/iterations/" + iterationID
	p.request("PUT", ip, `{"trigger_formula":{"constant":true},"score_review_threshold":100,"score_block_and_review_threshold":200,"score_decline_threshold":300}`, 200)
	for i, formula := range formulas {
		body, e := json.Marshal(map[string]any{"name": names[i], "formula": formula, "score_modifier": 1, "rule_group": "golden", "stable_rule_id": fmt.Sprintf("aggregate-%d", i)})
		check(e)
		p.request("POST", ip+"/rules", string(body), 201)
	}
	diagnosticOwner, err := mysql.NewAuthoringStore(p.db, p.database, p.tenant, 500, 2<<20, p.authorize)
	check(err)
	draft, err := diagnosticOwner.DraftPreparation(ctx, uuid.MustParse(parentID), uuid.MustParse(iterationID))
	check(err)
	check(service.ValidateDraftModel(draft.Scenario, draft.Iteration, draft.Rules, model, service.ExecutionPreparationLimits{MaxItems: 500, MaxBytes: 2 << 20}))
	p.request("POST", ip+"/commit", "{}", 200)
	p.request("POST", parentPath+"/publications", `{"action":"publish","iteration_id":"`+iterationID+`"}`, 200)
	owner, e := mysql.NewAuthoringStore(p.db, p.database, p.tenant, 500, 2<<20, p.authorize)
	check(e)
	var parent scenario.Scenario
	var iteration scenario.Iteration
	var rules []scenario.Rule
	check(owner.ReadSnapshot(ctx, func(r mysql.AuthoringRepositories) error {
		var e error
		parent, e = r.Scenarios.GetByID(ctx, p.tenant.String(), parentID)
		if e != nil {
			return e
		}
		iteration, e = r.Iterations.GetByID(ctx, p.tenant.String(), parentID, iterationID)
		if e != nil {
			return e
		}
		rules, e = r.Rules.ListByIteration(ctx, p.tenant.String(), parentID, iterationID)
		return e
	}))
	pgTx := postgres.NewTransactionManager(pg)
	_, e = pg.Exec(ctx, "INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'Golden',$2,'active',now(),now())", p.tenant, pgSchema)
	check(e)
	check(pgTx.Run(ctx, func(r ports.MutationStore) error {
		v := parent
		v.LiveIterationID = nil
		if _, e := r.Scenarios().Create(ctx, v); e != nil {
			return e
		}
		if _, e := r.Iterations().Create(ctx, iteration); e != nil {
			return e
		}
		for _, rule := range rules {
			if _, e := r.Rules().Create(ctx, rule); e != nil {
				return e
			}
		}
		return r.Scenarios().SetLiveIterationID(ctx, p.tenant.String(), parent.ID, parent.LiveIterationID)
	}))
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
	allowed := []string{"count", "count_distinct", "sum", "avg", "min", "max"}
	target := service.NewDecisionService(nil, IDs{}, Clock{}, nil, nil, nil, nil, targetReader, nil, nil, reads.Workflows(), reads.WorkflowRules(), nil, nil, nil, reads.Snoozes(), nil, reads.CustomLists(), reads.RecordTags(), reads.RiskSnapshots(), reads.IPFlags(), reads.ScreeningConfigs(), nil, reads.ScoringConfigs(), nil, false, "strict", allowed, 1, 1, 1, nil)
	source := service.NewDecisionService(nil, IDs{}, Clock{}, nil, nil, nil, nil, sourceReader, nil, nil, postgres.NewWorkflowRepository(pg), postgres.NewWorkflowRuleRepository(pg), nil, nil, nil, postgres.NewRuleSnoozeRepository(pg), nil, nil, nil, nil, nil, postgres.NewScreeningConfigRepository(pg), nil, postgres.NewScoringConfigRepository(pg), nil, false, "strict", allowed, 1, 1, 1, nil)
	limits := service.ExecutionPreparationLimits{MaxItems: 500, MaxBytes: 2 << 20}
	pin, e := service.NewPinnedScenarioSnapshot(parent, iteration, rules, limits)
	check(e)
	request := []service.DecisionEvaluationRequest{{ObjectID: "golden-0", ObjectType: object, Fields: map[string]any{"score": 10}}}
	queues := map[ports.JobKind]mysql.EffectQueuePolicy{}
	for _, kind := range []ports.JobKind{ports.JobWorkflow, ports.JobScreening, ports.JobScoring, ports.JobOutbox} {
		queues[kind] = mysql.EffectQueuePolicy{Name: string(kind), MaxAttempts: 3}
	}
	effects, e := mysql.NewQueuedEffectStore(p.db, p.database, p.tenant, 500, 2<<20, authority, nil, queues)
	check(e)
	unit, e := mysql.NewDecisionUnitOfWorkStore(effects, p.executions)
	check(e)
	results := make([]service.DecisionEvaluationResult, 2)
	for i, svc := range []service.DecisionService{source, target} {
		reader := sourceReader
		if i == 1 {
			reader = targetReader
		}
		batch, e := service.PrepareObjectBatch(ctx, p.tenant.String(), object, request, model, reader, limits)
		check(e)
		plans, e := svc.PreparePinnedBatch(ctx, pin, batch)
		check(e)
		if len(plans) != 1 {
			t.Fatal("missing plan")
		}
		persist := func(store ports.DecisionMutationStore) error {
			var e error
			results[i], e = plans[0].Persist(ctx, store)
			return e
		}
		if i == 0 {
			check(pgTx.Run(ctx, func(r ports.MutationStore) error { return persist(r) }))
		} else {
			check(unit.RunDecisionMutation(ctx, func(r ports.DecisionMutationStore) error { return persist(r) }))
		}
		if results[i].Decision == nil || results[i].Decision.Score != len(formulas) || len(results[i].RuleExecutions) != len(formulas) {
			raw, _ := json.Marshal(results[i])
			t.Fatalf("engine %d golden evidence: %s", i, raw)
		}
	}
	// Read committed evidence through the actual repositories on both engines.
	results[0].RuleExecutions, e = postgres.NewRuleExecutionRepository(pg).ListByDecision(ctx, p.tenant.String(), results[0].Decision.ID)
	check(e)
	check(history.ReadSnapshot(ctx, func(r mysql.DecisionRepositories) error {
		var err error
		results[1].RuleExecutions, err = r.RuleExecutions.ListByDecision(ctx, p.tenant.String(), results[1].Decision.ID)
		return err
	}))
	for i := range results {
		if len(results[i].RuleExecutions) != len(formulas) {
			t.Fatal("persisted evidence count mismatch")
		}
		sort.Slice(results[i].RuleExecutions, func(a, b int) bool { return results[i].RuleExecutions[a].RuleID < results[i].RuleExecutions[b].RuleID })
	}
	// Compare semantic persisted evidence, excluding generated IDs and timings.
	for i := range results[0].RuleExecutions {
		a, b := results[0].RuleExecutions[i], results[1].RuleExecutions[i]
		a.ID, b.ID = "", ""
		a.DecisionID, b.DecisionID = "", ""
		a.CreatedAt, b.CreatedAt = Clock{}.Now(), Clock{}.Now()
		// The rule result is the stable evaluator contract; timing fields are not.
		rawA, _ := json.Marshal(a)
		rawB, _ := json.Marshal(b)
		var va, vb map[string]any
		check(json.Unmarshal(rawA, &va))
		check(json.Unmarshal(rawB, &vb))
		for _, v := range []map[string]any{va, vb} {
			for k := range v {
				if strings.Contains(strings.ToLower(k), "time") || strings.Contains(strings.ToLower(k), "duration") || strings.Contains(strings.ToLower(k), "created") {
					delete(v, k)
				}
			}
		}
		if !reflect.DeepEqual(va, vb) {
			t.Fatalf("rule evidence mismatch: %s / %s", rawA, rawB)
		}
	}
	for _, reader := range []ports.TenantDataReader{sourceReader, targetReader} {
		for _, helper := range []string{"related_count", "related_records"} {
			_, err := ast_eval.EvaluateNode(ctx, domainast.Node{Function: helper, NamedChildren: map[string]domainast.Node{"object_type": {Constant: object}, "field": {Constant: "score"}}}, ast_eval.Runtime{TenantID: p.tenant.String(), ObjectType: object, TenantDataReader: reader})
			if err == nil || !strings.Contains(err.Error(), "disabled") {
				t.Fatalf("broad helper %s must fail explicitly: %v", helper, err)
			}
		}
		for _, kind := range []string{"stddev", "pctile", "median"} {
			if _, e := reader.AggregateRecords(ctx, p.tenant.String(), ports.AggregateQuery{ObjectType: object, Field: "score", Aggregate: kind}); e == nil {
				t.Fatal("unsupported aggregate accepted", kind)
			}
		}
	}
}
