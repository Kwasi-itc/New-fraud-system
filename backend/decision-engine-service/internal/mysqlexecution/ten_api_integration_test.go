package mysqlexecution

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/google/uuid"
)

func TestScoringAuthoringConcurrentReplacementSnapshotAndBounds(t *testing.T) {
	f := setupPreparation(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	authorize := func(context.Context, *sql.Tx, uuid.UUID, mysql.AuthoringAction) error {
		if f.revoked.Load() {
			return mysql.ErrDecisionAuthorizationDenied
		}
		return nil
	}
	owner, err := mysql.NewConfigurationStore(f.db, f.database, f.tenant, 500, 2<<20, authorize)
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewScoringAuthoring(owner, ids{}, clock{})
	if err != nil {
		t.Fatal(err)
	}
	fields := func(name string) scoring.Config {
		raw, _ := json.Marshal(map[string]string{"name": name})
		return scoring.Config{Name: name, RulesetRef: name, ConfigJSON: raw, AllowedOutcomes: []string{name, name}, Active: true}
	}
	v, err := a.Save(ctx, f.parent, uuid.Nil, fields("initial"))
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.MustParse(v.ID)
	check := func(v scoring.Config) error {
		var doc map[string]string
		if err := json.Unmarshal(v.ConfigJSON, &doc); err != nil {
			return err
		}
		if len(v.AllowedOutcomes) != 2 || v.AllowedOutcomes[0] != v.Name || v.AllowedOutcomes[1] != v.Name || v.RulesetRef != v.Name || doc["name"] != v.Name {
			return fmt.Errorf("torn configuration: %+v", v)
		}
		return nil
	}
	start := make(chan struct{})
	failures := make(chan error, 32)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			v, err := a.Save(ctx, f.parent, id, fields(fmt.Sprintf("writer-%d", i)))
			if err == nil {
				err = check(v)
			}
			if err != nil {
				failures <- err
			}
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 16; i++ {
			v, err := a.Get(ctx, f.parent, id)
			if err == nil {
				err = check(v)
			}
			if err != nil {
				failures <- err
				return
			}
		}
	}()
	close(start)
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := a.Save(ctx, f.parent, uuid.Nil, fields(fmt.Sprintf("extra-%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	limitedStore, err := mysql.NewConfigurationStore(f.db, f.database, f.tenant, 2, 2<<20, authorize)
	if err != nil {
		t.Fatal(err)
	}
	limited, err := NewScoringAuthoring(limitedStore, ids{}, clock{})
	if err != nil {
		t.Fatal(err)
	}
	if out, err := limited.List(ctx, f.parent); !errors.Is(err, mysql.ErrProjectionLimit) || out != nil {
		t.Fatal("bounded list truncated", out, err)
	}
	tooMany := fields("too many")
	tooMany.AllowedOutcomes = []string{"a", "b", "c"}
	if out, err := limited.Save(ctx, f.parent, uuid.Nil, tooMany); !errors.Is(err, mysql.ErrProjectionLimit) || out.ID != "" {
		t.Fatal(out, err)
	}
	foreignStore, err := mysql.NewConfigurationStore(f.db, f.database, uuid.New(), 500, 2<<20, authorize)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := NewScoringAuthoring(foreignStore, ids{}, clock{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := foreign.Get(ctx, f.parent, id); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("foreign read", err)
	}
	if foreign.MatchesExecutionStore(f.executions) || limited.MatchesExecutionStore(f.executions) || !a.MatchesExecutionStore(f.executions) {
		t.Fatal("scope matching failed")
	}
	f.revoked.Store(true)
	if _, err := a.List(ctx, f.parent); !errors.Is(err, mysql.ErrDecisionAuthorizationDenied) {
		t.Fatal(err)
	}
	if err := a.Delete(ctx, f.parent, id); !errors.Is(err, mysql.ErrDecisionAuthorizationDenied) {
		t.Fatal(err)
	}
}

func TestStandaloneValidationRechecksEditsRevocationAndLatestWithoutHistory(t *testing.T) {
	f := setupPreparation(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	authorize := func(context.Context, *sql.Tx, uuid.UUID, mysql.AuthoringAction) error {
		if f.revoked.Load() {
			return mysql.ErrDecisionAuthorizationDenied
		}
		return nil
	}
	owner, err := mysql.NewAuthoringStore(f.db, f.database, f.tenant, 500, 2<<20, authorize)
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
	f.provider.onRead = func() { f.revoked.Store(true) }
	if result, err := a.Validate(ctx, f.parent, id); !errors.Is(err, mysql.ErrDecisionAuthorizationDenied) || result.Valid {
		t.Fatal(result, err)
	}
	f.provider.onRead = nil
	f.revoked.Store(false)
	f.provider.onRead = func() {
		changed := draft
		changed.Schedule = "changed during external read"
		if _, err := a.UpdateDraft(ctx, f.parent, id, changed); err != nil {
			t.Fatal(err)
		}
	}
	if result, err := a.Validate(ctx, f.parent, id); !errors.Is(err, mysql.ErrExecutionBindingChanged) || result.Valid {
		t.Fatal(result, err)
	}
	f.provider.onRead = nil
	if result, err := a.Validate(ctx, f.parent, id); err != nil || !result.Valid {
		t.Fatal(result, err)
	}
	// More iterations than the bounded reader may list: latest-rule lookup
	// must still resolve the live link directly, then the newest version.
	for i := 0; i < 4; i++ {
		draft, err = a.CloneDraft(ctx, f.parent, f.old)
		if err != nil {
			t.Fatal(err)
		}
	}
	limitedOwner, err := mysql.NewAuthoringStore(f.db, f.database, f.tenant, 1, 2<<20, authorize)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := mysql.NewTenantDataReader(f.db, f.database, f.tenant, 1, 2<<20, f.provider, func(context.Context, *sql.Tx, uuid.UUID, bool) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	limited, err := NewAuthoring(limitedOwner, reader, ids{}, clock{}, service.ExecutionPreparationLimits{MaxItems: 1, MaxBytes: 2 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := limited.Iterations(ctx, f.parent); !errors.Is(err, mysql.ErrProjectionLimit) {
		t.Fatal("fixture did not exceed history cap", err)
	}
	rules, err := limited.LatestRules(ctx, f.parent)
	if err != nil || len(rules) != 1 || rules[0].IterationID != f.old.String() {
		t.Fatal(rules, err)
	}
	if _, err := owner.Unpublish(ctx, mysql.PublicationInput{ScenarioID: f.parent, IterationID: f.old, EventID: uuid.New()}); err != nil {
		t.Fatal(err)
	}
	rules, err = limited.LatestRules(ctx, f.parent)
	if err != nil || len(rules) != 1 || rules[0].IterationID != draft.ID {
		t.Fatal(rules, err)
	}
}
