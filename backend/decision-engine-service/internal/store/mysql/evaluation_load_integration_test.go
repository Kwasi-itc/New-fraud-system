package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/loadacceptance"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

// This fixture measures real service/SQL evaluation and durable job production.
// Model publication and authorization are explicit contract fixtures. It does
// not claim provider delivery, real-owner composition or production capacity.
type evaluationLoadProfile struct {
	Reference                  string                  `json:"direction_reference"`
	Seed                       string                  `json:"seed"`
	Tenants                    int                     `json:"tenants"`
	HotPercent                 int                     `json:"hot_percent"`
	Load                       loadacceptance.Profile  `json:"load"`
	DeferAdditionalScoring     bool                    `json:"defer_additional_scoring,omitempty"`
	History                    *loadacceptance.Profile `json:"history,omitempty"`
	AggregateRemoteConcurrency int                     `json:"aggregate_remote_concurrency,omitempty"`
}

type evaluationLoadModel struct {
	ports.DataModelReader
	tenant string
	model  ports.TenantModel
}

type evaluationLoadClock struct{}

func (evaluationLoadClock) Now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

func (m evaluationLoadModel) GetTenantModel(_ context.Context, tenant string) (ports.TenantModel, error) {
	if tenant != m.tenant {
		return ports.TenantModel{}, ErrTenantScope
	}
	return m.model, nil
}

func TestEvaluationLoadHarnessSmoke(t *testing.T) {
	p := evaluationLoadProfile{Reference: "smoke only", Seed: "evaluation-load-v1", Tenants: 10, HotPercent: 60, Load: loadacceptance.Profile{Duration: 10 * time.Second, Rate: 10, Workers: 4, Timeout: 5 * time.Second, P95: 5 * time.Second, P99: 5 * time.Second}}
	path := os.Getenv("DECISION_EVALUATION_SMOKE_OUTPUT")
	if path == "" {
		path = filepath.Join(t.TempDir(), "evaluation-smoke.json")
	}
	runEvaluationLoad(t, p, path, true)
}

func TestManualEvaluationLoadAcceptance(t *testing.T) {
	path := os.Getenv("DECISION_EVALUATION_LOAD_PROFILE")
	if path == "" {
		t.Skip("explicit evaluation profile required")
	}
	if os.Getenv("DECISION_MYSQL_TEST_ADDRESS") != "mysql:3306" || os.Getenv("DECISION_MYSQL_REQUIRE_INTEGRATION") != "1" {
		t.Fatal("private disposable compose fixture required")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || stat.Size() > 64<<10 {
		t.Fatal("invalid profile size", err)
	}
	decoder := json.NewDecoder(io.LimitReader(f, 64<<10))
	decoder.DisallowUnknownFields()
	var p evaluationLoadProfile
	if err := decoder.Decode(&p); err != nil {
		t.Fatal(err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatal("one profile object required", err)
	}
	runEvaluationLoad(t, p, os.Getenv("DECISION_EVALUATION_LOAD_OUTPUT"), false)
}

func TestEvaluationSharedHistoryLoadHarnessSmoke(t *testing.T) {
	history := loadacceptance.Profile{Duration: 10 * time.Second, Rate: 5, Workers: 2, Timeout: 5 * time.Second, P95: 5 * time.Second, P99: 5 * time.Second}
	p := evaluationLoadProfile{Reference: "mixed smoke only; not capacity acceptance", Seed: "mixed-evaluation-v1", Tenants: 10, HotPercent: 60, DeferAdditionalScoring: true, History: &history, Load: loadacceptance.Profile{Duration: 10 * time.Second, Rate: 5, Workers: 2, Timeout: 5 * time.Second, P95: 5 * time.Second, P99: 5 * time.Second}}
	runEvaluationLoad(t, p, filepath.Join(t.TempDir(), "mixed-smoke.json"), true)
}

func runEvaluationLoad(t *testing.T, p evaluationLoadProfile, path string, smoke bool) {
	t.Helper()
	if strings.TrimSpace(p.Reference) == "" || p.Seed == "" || len(p.Seed) > 128 || p.Tenants < 2 || p.Tenants > 100 || p.HotPercent < 1 || p.HotPercent > 99 {
		t.Fatal("bounded explicit evaluation profile required")
	}
	if err := p.Load.Validate(); err != nil {
		t.Fatal(err)
	}
	if p.History != nil {
		if err := p.History.Validate(); err != nil {
			t.Fatal(err)
		}
		if p.History.Duration != p.Load.Duration {
			t.Fatal("shared workloads must cover the same duration")
		}
	}
	if p.AggregateRemoteConcurrency == 0 {
		p.AggregateRemoteConcurrency = 1
	}
	if p.AggregateRemoteConcurrency < 1 || p.AggregateRemoteConcurrency > 8 {
		t.Fatal("aggregate concurrency must fit the eight-connection test budget")
	}
	output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute+p.Load.Duration)
	defer cancel()
	f := scenarioFoundationDB(t, true, true, true, true, true, true, true, true, true, true, true, true)
	if !strings.HasPrefix(f.database, "decision_target_") {
		t.Fatal("generated disposable target required")
	}
	f.db.SetMaxOpenConns(8)
	f.db.SetMaxIdleConns(8)
	schema, err := mysqlstore.Identifier(f.database)
	if err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	exec := func(q string, args ...any) { t.Helper(); _, err := f.db.ExecContext(ctx, q, args...); must(err) }
	// Consumer contract tables only; actual data-model owner installation and
	// publication are exercised separately by the shared-owner acceptance suite.
	for _, ddl := range []string{
		"data_model_physical_tables(tenant_id BINARY(16),table_id BINARY(16),physical_name VARCHAR(64),published_version BINARY(16),epoch BIGINT,write_state VARCHAR(10),active_operation BINARY(16),PRIMARY KEY(tenant_id,table_id))",
		"data_model_ingestion_contracts(tenant_id BINARY(16),table_id BINARY(16),table_name LONGTEXT,version BINARY(16),payload JSON,PRIMARY KEY(tenant_id,table_id))",
		"data_model_physical_fields(tenant_id BINARY(16),table_id BINARY(16),field_id BINARY(16),component VARCHAR(7),physical_name VARCHAR(64),PRIMARY KEY(tenant_id,table_id,field_id,component))",
	} {
		exec("CREATE TABLE " + schema + "." + ddl + " ENGINE=InnoDB")
	}
	type binding struct {
		tenant, parent uuid.UUID
		svc            service.DecisionService
		history        *DecisionStore
	}
	bindings := make([]binding, p.Tenants)
	namespace := uuid.NewSHA1(uuid.NameSpaceOID, []byte(p.Seed))
	source, err := NewScreeningSourceContract(f.database, 2<<20)
	must(err)
	for i := range bindings {
		b := &bindings[i]
		b.tenant = uuid.NewSHA1(namespace, []byte(fmt.Sprintf("tenant:%d", i)))
		b.parent = uuid.NewSHA1(namespace, []byte(fmt.Sprintf("scenario:%d", i)))
		must(scenarioInsert(ctx, f.db, f.database, b.tenant, b.parent, fmt.Sprintf("Load %d", i)))
		a := authoringStore(t, f, b.tenant, 100, 2<<20)
		draft := authoringDraft(b.tenant, b.parent)
		draft.ID = uuid.NewSHA1(namespace, []byte(fmt.Sprintf("iteration:%d", i))).String()
		review, block, decline := 5, 20, 30
		draft.ScoreReviewThreshold = &review
		draft.ScoreBlockAndReviewThreshold = &block
		draft.ScoreDeclineThreshold = &decline
		must(a.Mutate(ctx, func(r AuthoringRepositories) error {
			v, err := r.Iterations.NextVersion(ctx, b.tenant.String(), b.parent.String())
			if err != nil {
				return err
			}
			draft.Version = v
			if _, err = r.Iterations.Create(ctx, draft); err != nil {
				return err
			}
			for n := 0; n < 10; n++ {
				rule := authoringRule(b.tenant, draft.ID, n+1)
				rule.ID = uuid.NewSHA1(namespace, []byte(fmt.Sprintf("rule:%d:%d", i, n))).String()
				rule.ScoreModifier = 1
				rule.Formula = json.RawMessage(`{"function":"gte","children":[{"function":"Payload","children":[{"constant":"amount"}]},{"constant":1}]}`)
				if n >= 8 {
					agg := "COUNT"
					if n == 9 {
						agg = "SUM"
					}
					rule.Formula = json.RawMessage(fmt.Sprintf(`{"function":"gte","children":[{"function":"Aggregator","named_children":{"tableName":{"constant":"wallets"},"fieldName":{"constant":"amount"},"aggregator":{"constant":"%s"},"filters":{"function":"List","children":[{"function":"Filter","named_children":{"tableName":{"constant":"wallets"},"fieldName":{"constant":"amount"},"operator":{"constant":"="},"value":{"function":"Payload","children":[{"constant":"amount"}]}}}]}}},{"constant":1}]}`, agg))
				}
				if _, err = r.Rules.Create(ctx, rule); err != nil {
					return err
				}
			}
			_, err = r.Iterations.Commit(ctx, b.tenant.String(), b.parent.String(), draft.ID, time.Now().UTC().Truncate(time.Microsecond))
			return err
		}))
		_, err = a.Publish(ctx, PublicationInput{b.parent, uuid.MustParse(draft.ID), uuid.New(), uuid.New()}, func(context.Context, *sql.Tx, PublicationSnapshot) error { return nil })
		must(err)
		table := uuid.NewSHA1(namespace, []byte(fmt.Sprintf("table:%d", i)))
		version := uuid.NewSHA1(namespace, []byte(fmt.Sprintf("version:%d", i)))
		physical := "tenant_load_" + stripUUID(table)
		quoted, err := mysqlstore.Identifier(physical)
		must(err)
		doc := registryDocument{ID: table, Name: "wallets", Fields: map[string]registryField{}}
		mt := ports.TenantModelTable{ID: table.String(), Name: "wallets", PublishedVersion: version.String(), Fields: map[string]ports.TenantModelField{}}
		columns := map[string]string{}
		for _, name := range []string{"object_id", "amount"} {
			id := uuid.NewSHA1(namespace, []byte(fmt.Sprintf("field:%d:%s", i, name)))
			kind := "int"
			if name == "object_id" {
				kind = "string"
			}
			doc.Fields[name] = registryField{ID: id, Name: name, Type: kind, IsUnique: name == "object_id"}
			mt.Fields[name] = ports.TenantModelField{Name: name, Type: kind}
			column := "field_load_" + stripUUID(id) + "_value"
			columns[name], err = mysqlstore.Identifier(column)
			must(err)
			exec("INSERT INTO "+schema+".data_model_physical_fields VALUES(?,?,?,?,?)", mysqlstore.UUID(b.tenant), mysqlstore.UUID(table), mysqlstore.UUID(id), "value", column)
		}
		exec("CREATE TABLE " + schema + "." + quoted + "(" + columns["object_id"] + " VARCHAR(80) PRIMARY KEY," + columns["amount"] + " INT NOT NULL, KEY amount_lookup(" + columns["amount"] + ")) ENGINE=InnoDB")
		marks := make([]string, 100)
		args := make([]any, 0, 200)
		for n := range marks {
			marks[n] = "(?,?)"
			args = append(args, fmt.Sprint(n), n+1)
		}
		exec("INSERT INTO "+schema+"."+quoted+" VALUES "+strings.Join(marks, ","), args...)
		raw, err := json.Marshal(doc)
		must(err)
		exec("INSERT INTO "+schema+".data_model_physical_tables VALUES(?,?,?,?,1,'open',NULL)", mysqlstore.UUID(b.tenant), mysqlstore.UUID(table), physical, mysqlstore.UUID(version))
		exec("INSERT INTO "+schema+".data_model_ingestion_contracts VALUES(?,?,?,?,?)", mysqlstore.UUID(b.tenant), mysqlstore.UUID(table), "wallets", mysqlstore.UUID(version), mysqlstore.JSON(raw))
		model := evaluationLoadModel{tenant: b.tenant.String(), model: ports.TenantModel{RevisionID: version.String(), RecordLookupField: "object_id", Tables: map[string]ports.TenantModelTable{"wallets": mt}}}
		reader, err := NewTenantDataReader(f.db, f.database, b.tenant, 100, 2<<20, model, allowPlatformFixture)
		must(err)
		config, err := NewConfigurationStore(f.db, f.database, b.tenant, 100, 2<<20, allowAuthoringFixture)
		must(err)
		at := time.Now().UTC().Truncate(time.Microsecond)
		must(config.Mutate(ctx, func(r ConfigurationRepositories) error {
			if _, err := r.Workflows.Create(ctx, workflow.Definition{ID: uuid.NewString(), TenantID: b.tenant.String(), ScenarioID: b.parent.String(), Name: "Load workflow", ActionType: workflow.ActionTypeEmitEvent, ActionConfig: []byte(`{"url":"https://example.test/event"}`), AllowedOutcomes: []string{"review"}, Active: true, CreatedAt: at, UpdatedAt: at}); err != nil {
				return err
			}
			if _, err := r.ScreeningConfigs.Create(ctx, screening.Config{ID: uuid.NewString(), TenantID: b.tenant.String(), ScenarioID: b.parent.String(), Name: "Load screening", Provider: "fixture", ConfigJSON: []byte(`{"queries":[{"name":"Fixture Person","type":"person"}]}`), AllowedOutcomes: []string{"review"}, Active: true, CreatedAt: at, UpdatedAt: at}); err != nil {
				return err
			}
			if p.DeferAdditionalScoring {
				return nil
			}
			_, err := r.ScoringConfigs.Create(ctx, scoring.Config{ID: uuid.NewString(), TenantID: b.tenant.String(), ScenarioID: b.parent.String(), Name: "Load scoring", RulesetRef: "fixture", ConfigJSON: []byte(`{}`), AllowedOutcomes: []string{"review"}, Active: true, CreatedAt: at, UpdatedAt: at})
			return err
		}))
		effects, err := NewQueuedEffectStore(f.db, f.database, b.tenant, 500, 2<<20, allowPlatformFixture, &source, effectQueuePolicies())
		must(err)
		unit, err := NewDecisionUnitOfWorkStore(effects, executionTestStore(t, f, b.tenant))
		must(err)
		history, err := NewDecisionStore(f.db, f.database, b.tenant, 100, 2<<20, allowPlatformFixture)
		must(err)
		svc := service.NewDecisionService(nil, serviceIDsFixture{}, evaluationLoadClock{}, model, serviceScenarioFixture{store: a}, serviceIterationFixture{store: a}, serviceRuleFixture{store: a}, reader, nil, nil, serviceWorkflowFixture{store: config}, serviceWorkflowRuleFixture{store: config}, nil, nil, nil, serviceSnoozeFixture{store: history}, nil, nil, nil, nil, nil, serviceScreeningFixture{store: config}, nil, serviceScoringFixture{store: config}, nil, true, "strict", []string{"count", "sum"}, 1, 1, p.AggregateRemoteConcurrency, nil)
		b.svc, err = svc.WithDecisionTransactions(unit)
		must(err)
		b.history = history
	}
	before := f.db.Stats()
	sqlBefore, err := evaluationSQLSnapshot(ctx, f.db, f.database)
	must(err)
	serverBefore, err := evaluationServerSnapshot(ctx, f.db)
	must(err)
	stageMetrics := &evaluationStageMetrics{totals: map[string]evaluationStageTotal{}}
	restoreLogger := installEvaluationTimingLogger(stageMetrics)
	defer restoreLogger()
	var failureMu sync.Mutex
	failures := map[string]int{}
	type historyResult struct {
		report loadacceptance.Report
		err    error
	}
	historyDone := make(chan historyResult, 1)
	if p.History != nil {
		go func() {
			report, err := loadacceptance.Run(ctx, *p.History, func(ctx context.Context, index int) error {
				b := bindings[index%len(bindings)]
				return b.history.ReadSnapshot(ctx, func(r DecisionRepositories) error {
					rows, _, err := r.Decisions.ListFilteredCursor(ctx, b.tenant.String(), ports.DecisionListFilter{}, 25, nil)
					if err != nil {
						return err
					}
					for _, row := range rows {
						if row.TenantID != b.tenant.String() {
							return errors.New("shared history crossed tenant boundary")
						}
					}
					return nil
				})
			})
			historyDone <- historyResult{report, err}
		}()
	}
	report, runErr := loadacceptance.Run(ctx, p.Load, func(ctx context.Context, index int) (failure error) {
		defer func() {
			if failure == nil {
				return
			}
			category := "evaluation_error"
			var sqlError *mysqldriver.MySQLError
			if errors.As(failure, &sqlError) {
				category = fmt.Sprintf("mysql_%d", sqlError.Number)
			} else if errors.Is(failure, context.DeadlineExceeded) {
				category = "deadline"
			} else if errors.Is(failure, context.Canceled) {
				category = "canceled"
			}
			failureMu.Lock()
			failures[category]++
			failureMu.Unlock()
		}()
		tenant := 0
		if index*37%100 >= p.HotPercent {
			tenant = 1 + index%(p.Tenants-1)
		}
		b := bindings[tenant]
		result, err := b.svc.EvaluateScenario(ctx, b.tenant.String(), b.parent.String(), service.DecisionEvaluationRequest{ObjectID: fmt.Sprintf("LOAD-%09d", index), ObjectType: "wallets", Fields: map[string]any{"amount": index%100 + 1}})
		if err != nil {
			return err
		}
		if result.Decision == nil || result.Decision.TenantID != b.tenant.String() || result.Decision.ScenarioID != b.parent.String() || !result.Triggered || result.Decision.Score != 10 || result.Decision.Outcome != decision.OutcomeReview || len(result.RuleExecutions) != 10 {
			return errors.New("evaluation outcome/evidence mismatch")
		}
		return nil
	})
	var historyReport *loadacceptance.Report
	if p.History != nil {
		history := <-historyDone
		historyReport = &history.report
		runErr = errors.Join(runErr, history.err)
	}
	restoreLogger()
	sqlAfter, sqlErr := evaluationSQLSnapshot(ctx, f.db, f.database)
	serverAfter, serverErr := evaluationServerSnapshot(ctx, f.db)
	sqlDelta, deltaErr := evaluationSQLDelta(sqlBefore, sqlAfter)
	counts := map[string]int{}
	var countErr error
	// No worker drains these jobs: their exact persisted counts are the measured
	// producer contract. Provider throughput and receipt recovery remain separate.
	multipliers := map[string]int{"decision_engine_decisions": 1, "decision_engine_rule_executions": 10, "decision_engine_workflow_executions": 1, "decision_engine_screening_executions": 1, "decision_engine_scoring_requests": 1, "decision_engine_outbox_events": 4, "decision_engine_effect_job_receipts": 7, "decision_engine_effect_job_deliveries": 7}
	if p.DeferAdditionalScoring {
		multipliers["decision_engine_scoring_requests"] = 0
		multipliers["decision_engine_outbox_events"]--
		multipliers["decision_engine_effect_job_receipts"] -= 2
		multipliers["decision_engine_effect_job_deliveries"] -= 2
	}
	for table, multiplier := range multipliers {
		var count int
		err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+"."+table).Scan(&count)
		counts[table] = count
		if err != nil {
			countErr = errors.Join(countErr, err)
		} else if count != report.Succeeded*multiplier {
			countErr = errors.Join(countErr, fmt.Errorf("%s: persisted=%d expected=%d", table, count, report.Succeeded*multiplier))
		}
	}
	artifact := struct {
		Smoke                     bool                            `json:"smoke_only"`
		Scope                     string                          `json:"scope"`
		Profile                   evaluationLoadProfile           `json:"profile"`
		Load                      loadacceptance.Report           `json:"load"`
		History                   *loadacceptance.Report          `json:"history_load,omitempty"`
		Counts                    map[string]int                  `json:"persisted_counts"`
		FailureClasses            map[string]int                  `json:"failure_classes"`
		Stages                    map[string]evaluationStageTotal `json:"evaluation_stages"`
		SQL                       []evaluationSQLTotal            `json:"text_protocol_sql_workload_delta"`
		ServerBefore, ServerAfter map[string]uint64
		Before, After             sql.DBStats
	}{smoke, "actual decision service and SQL; fixture model/authority; 10 rules including two indexed SQL aggregates; scoring inclusion and concurrent bounded history reads follow profile; same eight-connection pool; no provider sends", p, report, historyReport, counts, failures, stageMetrics.totals, sqlDelta, serverBefore, serverAfter, before, f.db.Stats()}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	must(encoder.Encode(artifact))
	must(output.Sync())
	t.Logf("evaluation smoke=%t offered=%d success=%d p95=%s p99=%s counts=%v", smoke, report.Offered, report.Succeeded, report.P95, report.P99, counts)
	must(errors.Join(runErr, countErr, sqlErr, deltaErr, serverErr))
}
