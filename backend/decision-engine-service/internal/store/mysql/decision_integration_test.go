package mysql

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func decisionFixture(t *testing.T) (platformFixture, *DecisionStore, uuid.UUID, uuid.UUID, scenario.Iteration, scenario.Iteration, []scenario.Rule) {
	t.Helper()
	ctx := context.Background()
	f := scenarioFoundationDB(t, true, true)
	tenant, parent := uuid.New(), uuid.New()
	if err := scenarioInsert(ctx, f.db, f.database, tenant, parent, "Wallet Risk"); err != nil {
		t.Fatal(err)
	}
	a := authoringStore(t, f, tenant, 500, 2<<20)
	live := createAuthoringDraft(t, a, tenant, parent)
	phantom := createAuthoringDraft(t, a, tenant, parent)
	rules := []scenario.Rule{authoringRule(tenant, live.ID, 1), authoringRule(tenant, phantom.ID, 1)}
	if err := a.Mutate(ctx, func(r AuthoringRepositories) error {
		for _, v := range rules {
			if _, err := r.Rules.Create(ctx, v); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s, err := NewDecisionStore(f.db, f.database, tenant, 500, 2<<20, allowPlatformFixture)
	if err != nil {
		t.Fatal(err)
	}
	return f, s, tenant, parent, live, phantom, rules
}
func decisionInput(tenant, parent uuid.UUID, iteration string) decision.Decision {
	return decision.Decision{ID: uuid.NewString(), TenantID: tenant.String(), ScenarioID: parent.String(), ScenarioIterationID: iteration, ObjectID: "WALLET-%_AbC ", ObjectType: "wallets", RequestBody: []byte(`{"n":9007199254740993,"owner":null}`), Outcome: decision.OutcomeReview, Score: 45, Triggered: true, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
}
func TestMySQLDecisionPersistenceEvidenceAndRollback(t *testing.T) {
	f, s, tenant, parent, live, _, rules := decisionFixture(t)
	ctx := context.Background()
	v := decisionInput(tenant, parent, live.ID)
	evidence := make([]decision.RuleExecution, 120)
	for i := range evidence {
		evidence[i] = decision.RuleExecution{ID: uuid.NewString(), DecisionID: v.ID, RuleID: rules[0].ID, RuleName: "same hash/different owner", Outcome: "match", ScoreModifier: 45, CreatedAt: v.CreatedAt, Evaluation: &domainast.EvaluationNode{Function: "gte", ReturnValue: true}}
	}
	evidence[0].Evaluation = nil
	err := s.Mutate(ctx, func(r DecisionRepositories) error {
		stored, err := r.Decisions.Create(ctx, v)
		if err != nil {
			return err
		}
		if stored.Score != 45 || !stored.Triggered {
			t.Fatal(stored)
		}
		out, err := r.RuleExecutions.CreateMany(ctx, evidence)
		if err != nil {
			return err
		}
		if len(out) != 120 || out[0].ID != evidence[0].ID || out[119].ID != evidence[119].ID {
			t.Fatal("batch input order")
		}
		// Source typed nil is JSON null and returns an empty node, not SQL NULL.
		if out[0].Evaluation == nil || out[1].Evaluation.ReturnValue != true {
			t.Fatal("evaluation JSON semantics")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReadSnapshot(ctx, func(r DecisionRepositories) error {
		out, err := r.RuleExecutions.ListByDecision(ctx, tenant.String(), v.ID)
		if err != nil {
			return err
		}
		if len(out) != 120 {
			t.Fatal(len(out))
		}
		_, err = r.Decisions.GetByID(ctx, uuid.NewString(), v.ID)
		if !errors.Is(err, ErrTenantScope) {
			t.Fatal("tenant read", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Wrong iteration rule must fail the composite FK, rolling back the decision
	// and the first successfully inserted chunk, not merely the failing row.
	bad := decisionInput(tenant, parent, live.ID)
	badEvidence := append([]decision.RuleExecution(nil), evidence...)
	for i := range badEvidence {
		badEvidence[i].ID = uuid.NewString()
		badEvidence[i].DecisionID = bad.ID
	}
	badEvidence[110].RuleID = rules[1].ID
	if err := s.Mutate(ctx, func(r DecisionRepositories) error {
		if _, err := r.Decisions.Create(ctx, bad); err != nil {
			return err
		}
		_, err := r.RuleExecutions.CreateMany(ctx, badEvidence)
		return err
	}); err == nil {
		t.Fatal("foreign iteration accepted")
	}
	schema, _ := mysqlstore.Identifier(f.database)
	var count int
	if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".decision_engine_decisions WHERE id=?", mysqlstore.UUID(uuid.MustParse(bad.ID))).Scan(&count); err != nil || count != 0 {
		t.Fatal("decision not rolled back", count, err)
	}
	other, err := NewDecisionStore(f.db, f.database, uuid.New(), 500, 2<<20, allowPlatformFixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := other.Mutate(ctx, func(r DecisionRepositories) error {
		_, err := r.RuleExecutions.CreateMany(ctx, []decision.RuleExecution{{ID: uuid.NewString(), DecisionID: v.ID, RuleID: rules[0].ID, CreatedAt: v.CreatedAt}})
		return err
	}); err == nil {
		t.Fatal("cross tenant parent resolved")
	}
	if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".decision_engine_decisions SET score=0 WHERE id=?", mysqlstore.UUID(uuid.MustParse(v.ID))); err == nil {
		t.Fatal("history mutable")
	}
	overflow := v
	overflow.ID = uuid.NewString()
	overflow.Score = math.MaxInt32 + 1
	if err := s.Mutate(ctx, func(r DecisionRepositories) error { _, err := r.Decisions.Create(ctx, overflow); return err }); err == nil {
		t.Fatal("score overflow")
	}
}
func TestMySQLDecisionPagingSearchAndProjection(t *testing.T) {
	f, s, tenant, parent, live, _, _ := decisionFixture(t)
	ctx := context.Background()
	first := decisionInput(tenant, parent, live.ID)
	inputs := []decision.Decision{first, first, first}
	for i := range inputs {
		inputs[i].ID = uuid.NewString()
	}
	inputs[1].ObjectID = "Other"
	inputs[2].Score = -1
	if err := s.Mutate(ctx, func(r DecisionRepositories) error {
		for _, v := range inputs {
			if _, err := r.Decisions.Create(ctx, v); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReadSnapshot(ctx, func(r DecisionRepositories) error {
		all, err := r.Decisions.ListByTenant(ctx, tenant.String())
		if err != nil {
			return err
		}
		if len(all) != 3 || all[0].RequestBody != nil {
			t.Fatal("list includes request body")
		}
		page, more, err := r.Decisions.ListByTenantPage(ctx, tenant.String(), 2, 0)
		if err != nil {
			return err
		}
		if !more || !reflect.DeepEqual(page, all[:2]) {
			t.Fatal("first page")
		}
		cursor := &ports.DecisionListCursor{CreatedAt: page[1].CreatedAt, ID: page[1].ID}
		last, more, err := r.Decisions.ListFilteredCursor(ctx, tenant.String(), ports.DecisionListFilter{}, 2, cursor)
		if err != nil {
			return err
		}
		if more || len(last) != 1 || last[0].ID != all[2].ID {
			t.Fatal("cursor tie ordering")
		}
		count, err := r.Decisions.CountByObject(ctx, tenant.String(), first.ObjectType, first.ObjectID)
		if err != nil {
			return err
		}
		if count != 2 {
			t.Fatal("exact text", count)
		}
		empty, err := r.Decisions.CountByObject(ctx, tenant.String(), "", "")
		if err != nil {
			return err
		}
		if empty != 0 {
			t.Fatal("empty object identity broadened into a tenant-wide read")
		}
		count, err = r.Decisions.CountByObject(ctx, tenant.String(), first.ObjectType, "WALLET-%_abc ")
		if err != nil {
			return err
		}
		if count != 0 {
			t.Fatal("case-insensitive exact identity")
		}
		found, err := r.Decisions.ListFiltered(ctx, tenant.String(), ports.DecisionListFilter{Search: "wallet risk"})
		if err != nil {
			return err
		}
		if len(found) != 3 {
			t.Fatal("scenario search", len(found))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	tiny, err := NewDecisionStore(f.db, f.database, tenant, 500, 8, allowPlatformFixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := tiny.ReadSnapshot(ctx, func(r DecisionRepositories) error {
		_, err := r.Decisions.GetByID(ctx, tenant.String(), inputs[0].ID)
		return err
	}); !errors.Is(err, ErrProjectionLimit) {
		t.Fatal("oversized projection", err)
	}
	if err := s.ReadSnapshot(ctx, func(r DecisionRepositories) error { _, err := r.Decisions.Create(ctx, first); return err }); err == nil {
		t.Fatal("snapshot wrote history")
	}
}
func TestMySQLPhantomComparisonBindingAndExpiry(t *testing.T) {
	f, s, tenant, parent, live, phantom, rules := decisionFixture(t)
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Microsecond)
	run := scenario.TestRun{ID: uuid.NewString(), TenantID: tenant.String(), ScenarioID: parent.String(), LiveIterationID: live.ID, PhantomIterationID: phantom.ID, Status: scenario.TestRunStatusPending, CreatedAt: at, UpdatedAt: at, ExpiresAt: at.Add(time.Hour)}
	item := decision.PhantomDecision{ID: uuid.NewString(), TenantID: tenant.String(), ScenarioID: parent.String(), ScenarioIterationID: phantom.ID, TestRunID: run.ID, ObjectType: "wallets", ObjectID: "TEST", Outcome: decision.OutcomeReview, Score: 45, Triggered: true, CreatedAt: at}
	if err := s.Mutate(ctx, func(r DecisionRepositories) error {
		stored, err := r.TestRuns.Create(ctx, run)
		if err != nil {
			return err
		}
		if !stored.ExpiresAt.Equal(run.ExpiresAt) {
			t.Fatal("expiry changed")
		}
		if _, err := r.PhantomDecisions.Create(ctx, item); err != nil {
			return err
		}
		evidence := decision.PhantomRuleExecution{ID: uuid.NewString(), PhantomDecisionID: item.ID, RuleID: rules[1].ID, RuleName: "comparison", Outcome: "match", ScoreModifier: 45, CreatedAt: at}
		if _, err := r.PhantomRuleExecutions.CreateMany(ctx, []decision.PhantomRuleExecution{evidence}); err != nil {
			return err
		}
		stored, err = r.TestRuns.UpdateStatus(ctx, tenant.String(), run.ID, scenario.TestRunStatusUp, at)
		if err != nil {
			return err
		}
		if stored.Status != scenario.TestRunStatusUp {
			t.Fatal(stored)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	invalid := item
	invalid.ID = uuid.NewString()
	invalid.ScenarioIterationID = live.ID
	if err := s.Mutate(ctx, func(r DecisionRepositories) error { _, err := r.PhantomDecisions.Create(ctx, invalid); return err }); err == nil {
		t.Fatal("live iteration used as phantom")
	}
	if err := s.ReadSnapshot(ctx, func(r DecisionRepositories) error {
		comparisons, err := r.PhantomDecisions.ListByTestRun(ctx, tenant.String(), run.ID)
		if err != nil {
			return err
		}
		if len(comparisons) != 1 {
			t.Fatal(len(comparisons))
		}
		evidence, err := r.PhantomRuleExecutions.ListByPhantomDecision(ctx, tenant.String(), item.ID)
		if err != nil {
			return err
		}
		if len(evidence) != 1 || evidence[0].ScoreModifier != 45 {
			t.Fatal(evidence)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	schema, _ := mysqlstore.Identifier(f.database)
	if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".decision_engine_test_runs SET expires_at=UTC_TIMESTAMP(6) WHERE id=?", mysqlstore.UUID(uuid.MustParse(run.ID))); err == nil {
		t.Fatal("expiry identity mutable")
	}
}

func TestMySQLHistoryApprovedUnicodeSearchAndExactIDs(t *testing.T) {
	f, s, tenant, parent, live, _, _ := decisionFixture(t)
	ctx := context.Background()
	a := authoringStore(t, f, tenant, 500, 2<<20)
	if err := a.Mutate(ctx, func(r AuthoringRepositories) error {
		v, err := r.Scenarios.GetByID(ctx, tenant.String(), parent.String())
		if err != nil {
			return err
		}
		v.Name = "Café risk"
		_, err = r.Scenarios.Update(ctx, v)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	d := decisionInput(tenant, parent, live.ID)
	d.ObjectID = "PAY-É "
	if err := s.Mutate(ctx, func(r DecisionRepositories) error { _, err := r.Decisions.Create(ctx, d); return err }); err != nil {
		t.Fatal(err)
	}
	if err := s.ReadSnapshot(ctx, func(r DecisionRepositories) error {
		for _, v := range []struct {
			search string
			want   int
		}{{"CAFÉ", 1}, {"cafe", 0}, {"pay-é", 1}, {"PAY-E", 0}} {
			found, err := r.Decisions.ListFiltered(ctx, tenant.String(), ports.DecisionListFilter{Search: v.search})
			if err != nil {
				return err
			}
			if len(found) != v.want {
				t.Fatal(v, found)
			}
		}
		for _, id := range []string{"pay-é ", "PAY-É"} {
			n, err := r.Decisions.CountByObject(ctx, tenant.String(), d.ObjectType, id)
			if err != nil {
				return err
			}
			if n != 0 {
				t.Fatal("exact ID was folded/trimmed", id)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
