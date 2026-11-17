package service

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/lookup"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"testing"
	"time"
)

type inlineConfigFixture struct {
	nilScreeningConfigRepository
	configs []screening.Config
}

func (r inlineConfigFixture) ListActiveByScenario(context.Context, string, string) ([]screening.Config, error) {
	return r.configs, nil
}

type inlineLookupFixture struct {
	status   string
	matches  int
	partial  bool
	err      error
	calls    int
	deadline bool
}

func (f *inlineLookupFixture) Evaluate(ctx context.Context, in lookup.Input) (*lookup.Result, error) {
	f.calls++
	_, f.deadline = ctx.Deadline()
	return &lookup.Result{Status: f.status, MatchCount: f.matches, Partial: f.partial, Policy: in.Policy}, f.err
}
func (f *inlineLookupFixture) Persist(context.Context, pgx.Tx, *lookup.Result) error { return nil }

func TestInlineOutcomePolicyDistinguishesFailuresAndPreservesScore(t *testing.T) {
	for _, tc := range []struct {
		name, status, failure, base, force, want string
		matches                                  int
		partial, wantErr                         bool
		err                                      error
	}{
		{name: "clean", status: "no_candidates", failure: "error", base: "approve", force: "review", want: "approve"},
		{name: "hit", status: "candidates", failure: "error", base: "approve", force: "block_and_review", want: "block_and_review", matches: 1},
		{name: "decline never lowered", status: "candidates", failure: "error", base: "decline", force: "review", want: "decline", matches: 1},
		{name: "error abort", status: "failed", failure: "error", base: "approve", force: "review", wantErr: true, err: lookup.ErrUnavailable},
		{name: "timeout abort", status: "timed_out", failure: "error", base: "approve", force: "review", wantErr: true, err: lookup.ErrUnavailable},
		{name: "partial abort", status: "incomplete", failure: "error", base: "approve", force: "review", partial: true, wantErr: true},
		{name: "unknown status abort", status: "unknown", failure: "error", base: "approve", force: "review", wantErr: true},
		{name: "authority always abort", status: "failed", failure: "error", base: "approve", force: "review", wantErr: true, err: lookup.ErrAuthority},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rule := screening.AuthoringRule{ID: uuid.NewString(), Provider: "internal-watchlists", Profile: "exact-name-v1", DatasetSelection: []screening.DatasetRef{{Scope: "tenant", ID: "test-list"}}, ForcedOutcome: tc.force, Name: "Inline fixture", EntityType: "Person", NameField: "beneficiary_name", InboxID: uuid.NewString()}
			cfg := screening.Config{ID: rule.ID, Name: rule.Name, Provider: rule.Provider, Active: true, ReviewInboxID: &rule.InboxID, ConfigJSON: rule.ConfigJSON()}
			fake := &inlineLookupFixture{status: tc.status, matches: tc.matches, partial: tc.partial, err: tc.err}
			svc := DecisionService{InlineScreening: fake, InlinePolicy: InlinePolicy{Enabled: true, Failure: tc.failure, Timeout: time.Second}, idGen: fixedIDGenerator{}, clock: fixedClock{now: time.Now()}, screeningConfigRepo: inlineConfigFixture{configs: []screening.Config{cfg}}}
			item := decision.Decision{ID: uuid.NewString(), TenantID: uuid.NewString(), ScenarioID: uuid.NewString(), ObjectType: "transaction", ObjectID: "fixture", Outcome: decision.Outcome(tc.base), Score: 42}
			out, execs, evidence, e := svc.evaluateInlineScreenings(context.Background(), item, map[string]any{"beneficiary_name": "Fictional Person"})
			if tc.wantErr {
				if !errors.Is(e, ErrInlineEvaluation) {
					t.Fatal("expected abort", e)
				}
				return
			}
			if e != nil || string(out.Outcome) != tc.want || out.Score != 42 || len(execs) != 1 || execs[0].Status != screening.ExecutionStatusCompleted || len(evidence) != 1 || evidence[0].Policy.FinalOutcome != tc.want || !fake.deadline {
				t.Fatal(out, execs, evidence, e)
			}
			asynchronous, e := svc.buildScreeningExecutions(context.Background(), out, map[string]any{"beneficiary_name": "Fictional Person"})
			if e != nil || len(asynchronous) != 0 {
				t.Fatal("inline queued a second search", asynchronous, e)
			}
		})
	}
}

func TestInlineActivationRequiresExplicitFailurePolicy(t *testing.T) {
	if (InlinePolicy{Enabled: true, Failure: "block_and_review", Timeout: time.Second}).Validate() == nil {
		t.Fatal("unapproved failure policy accepted")
	}
	if (InlinePolicy{Enabled: true, Timeout: time.Second}).Validate() == nil {
		t.Fatal("unapproved failure default")
	}
	if (InlinePolicy{Enabled: true, Failure: "error", Timeout: time.Second}).Validate() != nil {
		t.Fatal("explicit policy denied")
	}
	if (InlinePolicy{}).Validate() != nil {
		t.Fatal("disabled default invalid")
	}
}
