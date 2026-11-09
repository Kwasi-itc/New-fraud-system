package mysqlexecution

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/google/uuid"
)

type unavailableAuthoringID struct{}

func (unavailableAuthoringID) New() uuid.UUID { return uuid.Nil }

func TestAuthoringVersionRollbackAndConcurrentDrafts(t *testing.T) {
	f := setupPreparation(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	limits := service.ExecutionPreparationLimits{MaxItems: 100, MaxBytes: 2 << 20}
	a, err := NewAuthoring(f.authoring, f.preparer.reader, ids{}, clock{}, limits)
	if err != nil {
		t.Fatal(err)
	}
	bad, err := NewAuthoring(f.authoring, f.preparer.reader, unavailableAuthoringID{}, clock{}, limits)
	if err != nil {
		t.Fatal(err)
	}
	before, err := a.Iterations(ctx, f.parent)
	if err != nil {
		t.Fatal(err)
	}
	max := 0
	for _, v := range before {
		if v.Version > max {
			max = v.Version
		}
	}
	if v, err := bad.CreateDraft(ctx, f.parent); err == nil || v.ID != "" {
		t.Fatal("failed ID allocation succeeded", v, err)
	}
	const count = 8
	results := make(chan scenario.Iteration, count)
	failures := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := a.CreateDraft(ctx, f.parent)
			if err != nil {
				failures <- err
			} else {
				results <- v
			}
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	seen := map[int]bool{}
	var selected scenario.Iteration
	for v := range results {
		if seen[v.Version] {
			t.Fatal("duplicate version", v.Version)
		}
		seen[v.Version] = true
		selected = v
	}
	for v := max + 1; v <= max+count; v++ {
		if !seen[v] {
			t.Fatal("version allocation failed to roll back", seen)
		}
	}
	if _, err := f.db.ExecContext(ctx, "CREATE TRIGGER "+f.schema+".fixture_commit_failure BEFORE UPDATE ON "+f.schema+".decision_engine_scenario_iterations FOR EACH ROW BEGIN IF NEW.status='committed' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fixture commit failure'; END IF; END"); err != nil {
		t.Fatal(err)
	}
	id := uuid.MustParse(selected.ID)
	if v, err := a.Commit(ctx, f.parent, id); err == nil || v.ID != "" {
		t.Fatal("failed commit returned success", v, err)
	}
	current, err := a.Iteration(ctx, f.parent, id)
	if err != nil || current.Status != scenario.IterationStatusDraft || current.CommittedAt != nil {
		t.Fatal("failed commit changed draft", current, err)
	}
}

func TestAuthoringCommitRechecksPreparedDraftAndAuthority(t *testing.T) {
	f := setupPreparation(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	owner, err := mysql.NewAuthoringStore(f.db, f.database, f.tenant, 500, 2<<20, func(_ context.Context, _ *sql.Tx, _ uuid.UUID, _ mysql.AuthoringAction) error {
		if f.revoked.Load() {
			return mysql.ErrDecisionAuthorizationDenied
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewAuthoring(owner, f.preparer.reader, ids{}, clock{}, service.ExecutionPreparationLimits{MaxItems: 100, MaxBytes: 2 << 20})
	if err != nil {
		t.Fatal(err)
	}
	draft, err := a.CloneDraft(ctx, f.parent, f.old)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.MustParse(draft.ID)
	rules, err := a.Rules(ctx, f.parent, id)
	if err != nil || len(rules) == 0 {
		t.Fatal(rules, err)
	}
	// This write completes DURING model retrieval. No database lock may be
	// retained across that external call, and the changed rule must not commit.
	f.provider.onRead = func() {
		changed := rules[0]
		changed.ScoreModifier++
		if _, err := a.SaveRule(ctx, f.parent, id, uuid.MustParse(changed.ID), changed); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := a.Commit(ctx, f.parent, id); !errors.Is(err, mysql.ErrExecutionBindingChanged) || got.ID != "" {
		t.Fatal("changed draft committed", got, err)
	}
	f.provider.onRead = nil
	got, err := a.Iteration(ctx, f.parent, id)
	if err != nil || got.Status != scenario.IterationStatusDraft {
		t.Fatal(got, err)
	}
	f.provider.onRead = func() { f.revoked.Store(true) }
	if got, err := a.Commit(ctx, f.parent, id); !errors.Is(err, mysql.ErrDecisionAuthorizationDenied) || got.ID != "" {
		t.Fatal("revoked write committed", got, err)
	}
	f.provider.onRead = nil
	before := f.provider.calls
	if _, err := a.Commit(ctx, f.parent, id); !errors.Is(err, mysql.ErrDecisionAuthorizationDenied) {
		t.Fatal(err)
	}
	if f.provider.calls != before {
		t.Fatal("model provider called before authoring authorization")
	}
	f.revoked.Store(false)
	f.provider.onRead = func() {
		if _, err := f.db.ExecContext(ctx, "UPDATE "+f.schema+".data_model_physical_tables SET write_state='closed'"); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := a.Commit(ctx, f.parent, id); !errors.Is(err, mysql.ErrPublishedModelUnavailable) || got.ID != "" {
		t.Fatal("closed registry committed", got, err)
	}
	f.provider.onRead = nil
	if _, err := f.db.ExecContext(ctx, "UPDATE "+f.schema+".data_model_physical_tables SET write_state='open'"); err != nil {
		t.Fatal(err)
	}
	committed, err := a.Commit(ctx, f.parent, id)
	if err != nil || committed.Status != scenario.IterationStatusCommitted {
		t.Fatal(committed, err)
	}
	if _, err := a.SaveRule(ctx, f.parent, id, uuid.MustParse(rules[0].ID), rules[0]); !errors.Is(err, mysql.ErrCommittedIteration) {
		t.Fatal("committed rule mutable", err)
	}
}
