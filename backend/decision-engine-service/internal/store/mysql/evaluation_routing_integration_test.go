package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/testsupport"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

func TestEvaluationRoutingFreshCoherentAuthorizedAndBounded(t *testing.T) {
	db, database, _, _ := testsupport.DecisionMySQLRuntimeDatabase(t)
	schema, _ := mysqlstore.Identifier(database)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tenant, parent := uuid.New(), uuid.New()
	if err := scenarioInsert(ctx, db, database, tenant, parent, "routing snapshot"); err != nil {
		t.Fatal(err)
	}
	// Explicit authority fixture; all configuration reads/mutations, isolation,
	// concurrent commits and bounds below use the real native owner repositories.
	var reads atomic.Int32
	var revoked, pause atomic.Bool
	entered, release := make(chan struct{}), make(chan struct{})
	auth := func(ctx context.Context, tx *sql.Tx, id uuid.UUID, action AuthoringAction) error {
		if id != tenant || revoked.Load() {
			return ErrDecisionAuthorizationDenied
		}
		if action == AuthoringRead {
			reads.Add(1)
			// The session default does not describe START TRANSACTION READ ONLY.
			// An attempted no-row write must be rejected by the actual transaction.
			_, writeErr := tx.ExecContext(ctx, "UPDATE "+schema+".decision_engine_scenarios SET name=name WHERE FALSE")
			var sqlErr *driver.MySQLError
			if !errors.As(writeErr, &sqlErr) || sqlErr.Number != 1792 {
				return errors.New("routing snapshot did not reject writes")
			}
			// Establish the repeatable-read snapshot before allowing a concurrent
			// owner edit to commit between authorization and routing projections.
			var name string
			if err := tx.QueryRowContext(ctx, "SELECT name FROM "+schema+".decision_engine_scenarios WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(parent)).Scan(&name); err != nil {
				return err
			}
			if pause.CompareAndSwap(true, false) {
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
		return nil
	}
	cfg, err := NewConfigurationStore(db, database, tenant, 500, 2<<20, auth)
	if err != nil {
		t.Fatal(err)
	}
	reader := EvaluationReaders{configuration: cfg}
	at := time.Now().UTC().Truncate(time.Microsecond)
	wf := workflow.Definition{ID: uuid.NewString(), TenantID: tenant.String(), ScenarioID: parent.String(), Name: "before", ActionType: workflow.ActionTypeEmitEvent, ActionConfig: []byte(`{"event":"routing"}`), AllowedOutcomes: []string{"review"}, Active: true, CreatedAt: at, UpdatedAt: at}
	sc := screening.Config{ID: uuid.NewString(), TenantID: tenant.String(), ScenarioID: parent.String(), Name: "before", Provider: "local-development", ConfigJSON: []byte(`{}`), AllowedOutcomes: []string{"review"}, Active: true, CreatedAt: at, UpdatedAt: at}
	so := scoring.Config{ID: uuid.NewString(), TenantID: tenant.String(), ScenarioID: parent.String(), Name: "before", RulesetRef: "local:decision-score-v1", ConfigJSON: []byte(`{}`), AllowedOutcomes: []string{"review"}, Active: true, CreatedAt: at, UpdatedAt: at}
	if err := cfg.Mutate(ctx, func(r ConfigurationRepositories) error {
		if _, err := r.Workflows.Create(ctx, wf); err != nil {
			return err
		}
		if _, err := r.ScreeningConfigs.Create(ctx, sc); err != nil {
			return err
		}
		_, err := r.ScoringConfigs.Create(ctx, so)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	read := func() (ports.EvaluationRoutingSnapshot, error) {
		return reader.ReadEvaluationRouting(ctx, tenant.String(), parent.String())
	}
	initial, err := read()
	if err != nil || len(initial.Workflows) != 1 || len(initial.ScreeningConfigs) != 1 || len(initial.ScoringConfigs) != 1 || reads.Load() != 1 {
		t.Fatal("one authorized routing snapshot", initial, reads.Load(), err)
	}
	pause.Store(true)
	type result struct {
		snapshot ports.EvaluationRoutingSnapshot
		err      error
	}
	done := make(chan result, 1)
	go func() { out, err := read(); done <- result{out, err} }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	wf.Name, sc.Name, so.Name = "after", "after", "after"
	editErr := cfg.Mutate(ctx, func(r ConfigurationRepositories) error {
		if _, err := r.Workflows.Update(ctx, wf); err != nil {
			return err
		}
		if _, err := r.ScreeningConfigs.Update(ctx, sc); err != nil {
			return err
		}
		_, err := r.ScoringConfigs.Update(ctx, so)
		return err
	})
	close(release)
	if editErr != nil {
		t.Fatal(editErr)
	}
	concurrent := <-done
	if concurrent.err != nil || concurrent.snapshot.Workflows[0].Name != "before" || concurrent.snapshot.ScreeningConfigs[0].Name != "before" || concurrent.snapshot.ScoringConfigs[0].Name != "before" {
		t.Fatal("mixed configuration revisions", concurrent)
	}
	fresh, err := read()
	if err != nil || fresh.Workflows[0].Name != "after" || fresh.ScreeningConfigs[0].Name != "after" || fresh.ScoringConfigs[0].Name != "after" {
		t.Fatal("routing cached across calls", fresh, err)
	}
	// Each individual repository projection fits; the complete snapshot does not.
	raw, err := json.Marshal(fresh)
	if err != nil {
		t.Fatal(err)
	}
	narrow, err := NewConfigurationStore(db, database, tenant, 500, len(raw)-1, auth)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := (EvaluationReaders{configuration: narrow}).ReadEvaluationRouting(ctx, tenant.String(), parent.String()); !errors.Is(err, ErrProjectionLimit) || out.TenantID != "" {
		t.Fatal("aggregate byte budget or partial result", out, err)
	}
	if _, err := reader.ReadEvaluationRouting(ctx, uuid.NewString(), parent.String()); !errors.Is(err, ErrTenantScope) {
		t.Fatal("foreign tenant", err)
	}
	// Structured rules take precedence without mixing legacy definitions.
	if err := cfg.Mutate(ctx, func(r ConfigurationRepositories) error {
		_, err := r.WorkflowRules.Create(ctx, workflow.Rule{ID: uuid.NewString(), TenantID: tenant.String(), ScenarioID: parent.String(), Name: "structured", Priority: 1, CreatedAt: at, UpdatedAt: at})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	structured, err := read()
	if err != nil || len(structured.WorkflowRules) != 1 || len(structured.Workflows) != 0 {
		t.Fatal("structured routing precedence", structured, err)
	}
	revoked.Store(true)
	if out, err := read(); !errors.Is(err, ErrDecisionAuthorizationDenied) || out.TenantID != "" {
		t.Fatal("revoked read or partial output", out, err)
	}
}
