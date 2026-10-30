package mysql

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/postgres"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/testsupport"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The shared corpus compares the existing source repository, not a re-created
// mock SQL contract. Target-only lease/atomic-job semantics have separate tests;
// this is not a claim of complete business-handler/runtime parity.
func TestExecutionPostgresMySQLDifferentialCorpus(t *testing.T) {
	dsn := testsupport.PostgresDatabaseURL(t)
	directory, err := filepath.Abs("../../migrations/metadata")
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.New("file://"+directory, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Up(); err != nil {
		t.Fatal(err)
	}
	if src, db := m.Close(); src != nil || db != nil {
		t.Fatal(src, db)
	}
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	_, target, tenant, parent, iteration := executionFixture(t)
	scheduled := scheduledInput(tenant, parent, iteration)
	async := asyncInput(tenant, parent)
	if err := postgres.NewTransactionManager(pg).Run(ctx, func(s ports.MutationStore) error {
		at := scheduled.CreatedAt
		if _, err := s.Scenarios().Create(ctx, scenario.Scenario{ID: parent.String(), TenantID: tenant.String(), Name: "Execution fixture", TriggerObjectType: "wallets", CreatedAt: at, UpdatedAt: at}); err != nil {
			return err
		}
		draft := authoringDraft(tenant, parent)
		draft.ID, draft.Version, draft.CreatedAt = iteration.String(), 1, at
		_, err := s.Iterations().Create(ctx, draft)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	type snapshot struct {
		Scheduled                    []execution.ScheduledExecution
		Async                        []execution.AsyncDecisionExecution
		ScheduledCounts, AsyncCounts map[execution.Status]int
	}
	canonical := func(v any) any {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	var sourceOut, targetOut snapshot
	sourceScheduled := postgres.NewScheduledExecutionRepository(pg)
	sourceAsync := postgres.NewAsyncDecisionExecutionRepository(pg)
	for i := 0; i < 2; i++ {
		x, y := scheduled, async
		if i > 0 {
			x.ID, y.ID = uuid.NewString(), uuid.NewString()
			x.RequestBody, y.RequestBody = []byte(`{"changed":true}`), []byte(`{"changed":true}`)
		}
		ps, err := sourceScheduled.Create(ctx, x)
		if err != nil {
			t.Fatal(err)
		}
		pa, err := sourceAsync.Create(ctx, y)
		if err != nil {
			t.Fatal(err)
		}
		sourceOut.Scheduled, sourceOut.Async = append(sourceOut.Scheduled, ps), append(sourceOut.Async, pa)
		if err := target.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error {
			ms, err := r.Scheduled.Create(ctx, x)
			if err != nil {
				return err
			}
			ma, err := r.Async.Create(ctx, y)
			if err != nil {
				return err
			}
			targetOut.Scheduled, targetOut.Async = append(targetOut.Scheduled, ms.Record.ScheduledExecution), append(targetOut.Async, ma.Record.AsyncDecisionExecution)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	sourceOut.ScheduledCounts, err = sourceScheduled.CountByStatus(ctx, tenant.String(), parent.String())
	if err != nil {
		t.Fatal(err)
	}
	sourceOut.AsyncCounts, err = sourceAsync.CountByStatus(ctx, tenant.String())
	if err != nil {
		t.Fatal(err)
	}
	if err := target.Run(ctx, ExecutionRead, func(r ExecutionRepositories) error {
		var err error
		targetOut.ScheduledCounts, err = r.Scheduled.CountByStatus(ctx, tenant.String(), parent.String())
		if err != nil {
			return err
		}
		targetOut.AsyncCounts, err = r.Async.CountByStatus(ctx, tenant.String())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(canonical(sourceOut), canonical(targetOut)) {
		t.Fatalf("execution corpus differs\nsource=%#v\ntarget=%#v", sourceOut, targetOut)
	}
}
