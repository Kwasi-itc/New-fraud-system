package mysql

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestMySQLScenarioCopyAtomicPublicationAndTenantIsolation(t *testing.T) {
	f := scenarioFoundationDB(t, true, true, true, true, true, true, true, true)
	ctx := context.Background()
	tenant, source := uuid.New(), uuid.New()
	if err := scenarioInsert(ctx, f.db, f.database, tenant, source, "Source copy fixture"); err != nil {
		t.Fatal(err)
	}
	s := authoringStore(t, f, tenant, 1000, 2<<20)
	iteration := createAuthoringDraft(t, s, tenant, source)
	if err := s.Mutate(ctx, func(r AuthoringRepositories) error {
		rules := make([]scenario.Rule, 120)
		for i := range rules {
			rules[i] = authoringRule(tenant, iteration.ID, i+1)
		}
		_, err := r.Rules.CreateMany(ctx, tenant.String(), source.String(), iteration.ID, rules)
		if err != nil {
			return err
		}
		_, err = r.Iterations.Commit(ctx, tenant.String(), source.String(), iteration.ID, time.Now().UTC().Truncate(time.Microsecond))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	publicationGuard := func(context.Context, *sql.Tx, PublicationSnapshot) error { return nil } // explicit fixture, not production preparation
	if _, err := s.Publish(ctx, PublicationInput{ScenarioID: source, IterationID: uuid.MustParse(iteration.ID), EventID: uuid.New(), ReplacedEventID: uuid.New()}, publicationGuard); err != nil {
		t.Fatal(err)
	}
	config, err := NewConfigurationStore(f.db, f.database, tenant, 1000, 2<<20, allowAuthoringFixture)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	wr := workflow.Rule{ID: uuid.NewString(), TenantID: tenant.String(), ScenarioID: source.String(), Name: "Copy me", CreatedAt: at, UpdatedAt: at}
	if err := config.Mutate(ctx, func(r ConfigurationRepositories) error {
		if _, err := r.WorkflowRules.Create(ctx, wr); err != nil {
			return err
		}
		if _, err := r.WorkflowConditions.Create(ctx, workflow.Condition{ID: uuid.NewString(), TenantID: tenant.String(), RuleID: wr.ID, Function: workflow.ConditionAlways, Params: []byte(`{}`), CreatedAt: at, UpdatedAt: at}); err != nil {
			return err
		}
		_, err := r.WorkflowActions.Create(ctx, workflow.Action{ID: uuid.NewString(), TenantID: tenant.String(), RuleID: wr.ID, ActionType: workflow.ActionTypeDoNothing, ActionConfig: []byte(`{}`), CreatedAt: at, UpdatedAt: at})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	dependencies := func(context.Context, *sql.Tx, ScenarioCopySnapshot) error { return nil } // no case references in this fixture
	denied := errors.New("copy dependency denied")
	input := ScenarioCopyInput{SourceID: source, DestinationID: uuid.New(), Name: "Copied fixture"}
	if _, err := s.CopyScenario(ctx, input, func(context.Context, *sql.Tx, ScenarioCopySnapshot) error { return denied }, publicationGuard); !errors.Is(err, denied) {
		t.Fatal(err)
	}
	if _, err := s.CopyScenario(ctx, input, dependencies, func(context.Context, *sql.Tx, PublicationSnapshot) error { return denied }); !errors.Is(err, denied) {
		t.Fatal(err)
	}
	schema, _ := mysqlstore.Identifier(f.database)
	var count int
	if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".decision_engine_scenarios WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(input.DestinationID)).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial copy survived", count, err)
	}
	created, err := s.CopyScenario(ctx, input, dependencies, publicationGuard)
	if err != nil || created.LiveIterationID == nil || *created.LiveIterationID == iteration.ID {
		t.Fatal(created, err)
	}
	if err := s.ReadSnapshot(ctx, func(r AuthoringRepositories) error {
		rules, err := r.Rules.ListByIteration(ctx, tenant.String(), created.ID, *created.LiveIterationID)
		if err == nil && (len(rules) != 120 || rules[0].ScoreModifier != 35) {
			t.Fatal("rule copy", rules)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := config.ReadSnapshot(ctx, func(r ConfigurationRepositories) error {
		graphs, err := r.WorkflowRules.ListStructured(ctx, tenant.String(), created.ID)
		if err == nil && (len(graphs) != 1 || len(graphs[0].Conditions) != 1 || len(graphs[0].Actions) != 1 || graphs[0].Rule.ID == wr.ID) {
			t.Fatal("workflow copy", graphs)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	foreign := authoringStore(t, f, uuid.New(), 1000, 2<<20)
	if _, err := foreign.CopyScenario(ctx, ScenarioCopyInput{SourceID: source, DestinationID: uuid.New()}, dependencies, publicationGuard); err == nil {
		t.Fatal("cross-tenant copy")
	}
	if _, err := s.CopyScenario(ctx, input, dependencies, publicationGuard); err == nil {
		t.Fatal("duplicate destination treated as successful replay")
	}
	// Workflow-only copy reads no iteration/decision histories, validates the
	// actual destination, and rolls back if dependency preparation fails.
	destination := uuid.New()
	if err := scenarioInsert(ctx, f.db, f.database, tenant, destination, "Workflow-only destination"); err != nil {
		t.Fatal(err)
	}
	workflowGuard := func(_ context.Context, _ *sql.Tx, snapshot ScenarioCopySnapshot, target scenario.Scenario) error {
		if target.ID != destination.String() || len(snapshot.Iterations) != 0 || len(snapshot.Rules) != 0 || len(snapshot.Workflows) != 1 {
			return errors.New("wrong workflow-only snapshot/destination")
		}
		return nil
	}
	if _, err := config.CopyWorkflowRules(ctx, source, destination, func(context.Context, *sql.Tx, ScenarioCopySnapshot, scenario.Scenario) error { return denied }); !errors.Is(err, denied) {
		t.Fatal(err)
	}
	graphs, err := config.CopyWorkflowRules(ctx, source, destination, workflowGuard)
	if err != nil || len(graphs) != 1 || graphs[0].Rule.ScenarioID != destination.String() || graphs[0].Rule.ID == wr.ID {
		t.Fatal("workflow-only copy", graphs, err)
	}
}
