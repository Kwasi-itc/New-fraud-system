package mysql

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestMySQLConfigurationOrderedArraysAndAtomicReplacement(t *testing.T) {
	f := scenarioFoundationDB(t, true, true, true, true, true, true, true, true)
	ctx := context.Background()
	tenant, parent := uuid.New(), uuid.New()
	if err := scenarioInsert(ctx, f.db, f.database, tenant, parent, "Configurations"); err != nil {
		t.Fatal(err)
	}
	s, err := NewConfigurationStore(f.db, f.database, tenant, 500, 2<<20, allowAuthoringFixture)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	outcomes := []string{"review", "approve", "review", ""}
	wf := workflow.Definition{ID: uuid.NewString(), TenantID: tenant.String(), ScenarioID: parent.String(), Name: "Ordered workflow", Description: "configuration", ActionType: workflow.ActionTypeEmitEvent, ActionConfig: []byte(`{"event":"review"}`), AllowedOutcomes: outcomes, Active: true, CreatedAt: at, UpdatedAt: at}
	route := uuid.NewString()
	screen := screening.Config{ID: uuid.NewString(), TenantID: tenant.String(), ScenarioID: parent.String(), Name: "Screen", Provider: "provider", ConfigJSON: []byte(`{"a":1}`), AllowedOutcomes: outcomes, ReviewInboxID: &route, Active: true, CreatedAt: at, UpdatedAt: at}
	score := scoring.Config{ID: uuid.NewString(), TenantID: tenant.String(), ScenarioID: parent.String(), Name: "Score", RulesetRef: "rules", ConfigJSON: []byte(`{"a":2}`), AllowedOutcomes: []string{}, Active: false, CreatedAt: at, UpdatedAt: at}
	if err := s.Mutate(ctx, func(r ConfigurationRepositories) error {
		w, err := r.Workflows.Create(ctx, wf)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(w.AllowedOutcomes, outcomes) {
			t.Fatal(w)
		}
		sc, err := r.ScreeningConfigs.Create(ctx, screen)
		if err != nil {
			return err
		}
		if sc.ReviewInboxID == nil || *sc.ReviewInboxID != route || !reflect.DeepEqual(sc.AllowedOutcomes, outcomes) {
			t.Fatal(sc)
		}
		so, err := r.ScoringConfigs.Create(ctx, score)
		if err != nil {
			return err
		}
		if so.AllowedOutcomes == nil || len(so.AllowedOutcomes) != 0 {
			t.Fatal("empty array lost")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("rollback replacements")
	changed := wf
	changed.Name = "new"
	changed.AllowedOutcomes = []string{"decline"}
	if err := s.Mutate(ctx, func(r ConfigurationRepositories) error {
		if _, err := r.Workflows.Update(ctx, changed); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if err := s.ReadSnapshot(ctx, func(r ConfigurationRepositories) error {
		listed, err := r.Workflows.ListActiveByScenario(ctx, tenant.String(), parent.String())
		if err != nil {
			return err
		}
		if len(listed) != 1 || listed[0].Name != wf.Name || !reflect.DeepEqual(listed[0].AllowedOutcomes, outcomes) {
			t.Fatal("replacement not atomic")
		}
		scores, err := r.ScoringConfigs.ListActiveByScenario(ctx, tenant.String(), parent.String())
		if err != nil {
			return err
		}
		if len(scores) != 0 {
			t.Fatal("inactive score returned")
		}
		_, err = r.ScreeningConfigs.GetByID(ctx, uuid.NewString(), parent.String(), screen.ID)
		if !errors.Is(err, ErrTenantScope) {
			t.Fatal("tenant leak", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Storage preserves SQL NULL elements; public []string cannot represent them
	// and must fail rather than silently discard or replace the element.
	schema, _ := mysqlstore.Identifier(f.database)
	if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".decision_engine_workflow_outcomes SET value=NULL WHERE tenant_id=? AND config_id=? AND position=0", mysqlstore.UUID(tenant), mysqlstore.UUID(uuid.MustParse(wf.ID))); err != nil {
		t.Fatal(err)
	}
	var null bool
	if err := f.db.QueryRowContext(ctx, "SELECT value IS NULL FROM "+schema+".decision_engine_workflow_outcomes WHERE tenant_id=? AND config_id=? AND position=0", mysqlstore.UUID(tenant), mysqlstore.UUID(uuid.MustParse(wf.ID))).Scan(&null); err != nil || !null {
		t.Fatal("NULL storage lost", err)
	}
	if err := s.ReadSnapshot(ctx, func(r ConfigurationRepositories) error {
		_, err := r.Workflows.GetByID(ctx, tenant.String(), parent.String(), wf.ID)
		return err
	}); err == nil {
		t.Fatal("NULL silently converted")
	}
}
