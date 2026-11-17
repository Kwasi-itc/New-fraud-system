package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/internalprovider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/lookup"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestInlineFacadeAtomicEvidenceHistoricalReplayAndTimeout(t *testing.T) {
	f := newInternalFixture(t)
	ctx := context.Background()
	tenant := f.a.Tenant().String()
	scenario, iteration, config := uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := f.db.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`INSERT INTO core.scenarios(id,tenant_id,name,trigger_object_type,created_at,updated_at) VALUES($1,$2,'Inline fixture','transaction',now(),now())`, scenario, tenant)
	exec(`INSERT INTO core.scenario_iterations(id,scenario_id,tenant_id,version,status,trigger_formula,created_at) VALUES($1,$2,$3,1,'live','{"constant":true}',now())`, iteration, scenario, tenant)
	exec(`UPDATE core.scenarios SET live_iteration_id=$1 WHERE id=$2`, iteration, scenario)
	var fixture struct {
		Version int                 `json:"version"`
		Source  string              `json:"source"`
		Records []sourcedata.Record `json:"records"`
	}
	if e := json.Unmarshal(f.input, &fixture); e != nil {
		t.Fatal(e)
	}
	var person sourcedata.Record
	for _, r := range fixture.Records {
		if r.Type == "Person" {
			person = r
			break
		}
	}
	selection := sourcedata.Selection{Version: 2, Profile: sourcedata.Profile, Threshold: 100, Datasets: []sourcedata.DatasetRef{{Scope: "tenant", ID: f.private.Dataset}}}
	configRaw, _ := json.Marshal(map[string]any{"authoring_version": 2, "provider_config": selection, "forced_outcome": "review"})
	exec(`INSERT INTO core.screening_configs(id,tenant_id,scenario_id,name,allowed_outcomes,provider,config_json,active,created_at,updated_at) VALUES($1,$2,$3,'Inline fixture','{approve}','internal-watchlists',$4,true,now(),now())`, config, tenant, scenario, configRaw)
	request := internalRequest(person.Name, "Person", selection.Datasets...)
	request.DecisionID = uuid.NewString()
	request.ScenarioID = scenario
	request.ScreeningConfigID = config
	request.IdempotencyKey = uuid.NewString()
	request.ObjectType = "transaction"
	request.ObjectID = "inline-fixture"
	requestRaw, _ := json.Marshal(request)
	input := lookup.Input{TenantID: tenant, ScenarioID: scenario, ConfigID: config, DecisionID: request.DecisionID, ExecutionID: request.IdempotencyKey, Config: configRaw, Request: requestRaw, Policy: lookup.Policy{Version: 1, Failure: "error", TimeoutMS: 1000, ForcedOutcome: "review", OriginalOutcome: "approve", FinalOutcome: "review"}}
	facade := lookup.Facade{DB: f.db}
	result, e := facade.Evaluate(ctx, input)
	if e != nil || result.Status != "candidates" || result.MatchCount != 1 {
		t.Fatal("inline", result, e)
	}
	providerResult, e := (internalprovider.Provider{DB: f.db}).Search(ctx, f.a, request)
	if e != nil || len(providerResult.Matches) != result.MatchCount {
		t.Fatal("manual and inline disagree", e)
	}
	persist := func(commit bool) error {
		tx, e := f.db.Begin(ctx)
		if e != nil {
			return e
		}
		defer tx.Rollback(ctx)
		if _, e = tx.Exec(ctx, `INSERT INTO core.decisions(id,tenant_id,scenario_id,scenario_iteration_id,object_id,object_type,outcome,score,triggered,created_at) VALUES($1,$2,$3,$4,$5,$6,'review',0,true,now())`, input.DecisionID, tenant, scenario, iteration, request.ObjectID, request.ObjectType); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO core.screening_executions(id,tenant_id,config_id,decision_id,scenario_id,status,request_json,created_at) VALUES($1,$2,$3,$4,$5,'completed',$6,now())`, input.ExecutionID, tenant, config, input.DecisionID, scenario, requestRaw); e != nil {
			return e
		}
		if e = facade.Persist(ctx, tx, result); e != nil {
			return e
		}
		if commit {
			return tx.Commit(ctx)
		}
		return tx.Rollback(ctx)
	}
	if e = persist(false); e != nil {
		t.Fatal("rollback path", e)
	}
	var count int
	if e = f.db.QueryRow(ctx, `SELECT count(*) FROM screening.inline_evidence`).Scan(&count); e != nil || count != 0 {
		t.Fatal("rolled back evidence leaked", count, e)
	}
	if e = persist(true); e != nil {
		t.Fatal("atomic persist", e)
	}
	var evidence, policy []byte
	var status, source string
	if e = f.db.QueryRow(ctx, `SELECT provider_evidence,policy_snapshot,lookup_status,screening_id::text FROM screening.inline_evidence WHERE tenant_id=$1 AND execution_id=$2`, tenant, input.ExecutionID).Scan(&evidence, &policy, &status, &source); e != nil || status != "candidates" || !bytes.Contains(evidence, []byte(f.version.String())) || !bytes.Contains(policy, []byte(`"final_outcome": "review"`)) {
		t.Fatal(string(evidence), string(policy), status, e)
	}
	if _, e = f.db.Exec(ctx, `UPDATE screening.inline_evidence SET lookup_status='no_candidates' WHERE tenant_id=$1`, tenant); e == nil {
		t.Fatal("mutable historical evidence")
	}
	if _, e = f.db.Exec(ctx, `UPDATE screening.screenings SET status='pending',response_json='{}' WHERE id=$1`, source); e == nil {
		t.Fatal("historical decision requeued")
	}
	// A later removal changes current search, never the saved decision evidence.
	next := fixture
	next.Records = nil
	for _, r := range fixture.Records {
		if r.ID != person.ID {
			next.Records = append(next.Records, r)
		}
	}
	raw, _ := json.Marshal(next)
	ingest := sourceingest.Store{DB: f.db}
	gen, e := ingest.Stage(ctx, f.private, bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ingest.Publish(ctx, f.private, gen); e != nil {
		t.Fatal(e)
	}
	current, e := facade.Evaluate(ctx, input)
	if e != nil || current.Status != "no_candidates" {
		t.Fatal("removal", current, e)
	}
	var after []byte
	if e = f.db.QueryRow(ctx, `SELECT provider_evidence FROM screening.inline_evidence WHERE tenant_id=$1`, tenant).Scan(&after); e != nil || !bytes.Equal(evidence, after) {
		t.Fatal("historical evidence rewritten", e)
	}
	if _, e = (internalprovider.Provider{DB: f.db}).Enrich(ctx, f.a, sourcedata.CandidateRef{Dataset: selection.Datasets[0], Generation: f.version, RecordID: person.ID}); e != nil {
		t.Fatal("historical source unavailable", e)
	}
	foreign := input
	foreign.TenantID = f.b.Tenant().String()
	if _, e = facade.Evaluate(ctx, foreign); !errors.Is(e, lookup.ErrAuthority) {
		t.Fatal("foreign config", e)
	}
	// The deadline includes connection acquisition and authorization, not just SQL.
	cfg := f.db.Config()
	cfg.MaxConns = 1
	cfg.MinConns = 0
	one, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer one.Close()
	held, e := one.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	short := input
	short.Policy.TimeoutMS = 20
	started := time.Now()
	timed, e := (lookup.Facade{DB: one}).Evaluate(ctx, short)
	held.Release()
	if e == nil || timed.Status != "timed_out" || time.Since(started) > time.Second {
		t.Fatal("timeout not bounded", timed, e)
	}
	// Configuration/grant revocation before commit aborts all decision writes.
	exec(`DELETE FROM screening.source_grants WHERE tenant_id=$1 AND owner_scope=$1 AND dataset_id=$2`, tenant, f.private.Dataset)
	if _, e = facade.Evaluate(ctx, input); e == nil {
		t.Fatal("revoked grant matched")
	}
	_ = pgx.ErrNoRows
}
