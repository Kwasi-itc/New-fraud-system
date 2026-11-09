package mysql

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/mysqlpublication"
	"github.com/google/uuid"
)

func TestScreeningOwnerMutationsAndProjectionDrain(t *testing.T) {
	f := effectsFixture(t)
	installScreeningOwner(t, f)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.database)
	d := decisionInput(f.tenant, f.parent, f.iteration.ID)
	execution := f.screeningInput(d)
	source := uuid.New()
	if err := f.store.Mutate(ctx, func(r EffectRepositories) error {
		if _, err := r.Decisions.Create(ctx, d); err != nil {
			return err
		}
		_, err := r.ScreeningExecutions.CreateMany(ctx, []screening.Execution{execution})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := f.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO "+schema+`.screening_screenings(id,tenant_id,decision_id,scenario_id,screening_config_id,execution_id,status,provider,object_type,object_id,request_json,response_json,decision_projection,provider_reference,last_error,created_at,updated_at) VALUES(?,?,?,?,?,?,'queued','fixture','account','one','{}','{}','{}','','',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, mysqlstore.UUID(source), mysqlstore.UUID(f.tenant), mysqlstore.UUID(uuid.MustParse(d.ID)), mysqlstore.UUID(f.parent), mysqlstore.UUID(uuid.MustParse(f.screen.ID)), mysqlstore.UUID(uuid.MustParse(execution.ID)))
	// Explicit test service authority; production service-principal grants are
	// separate D19.21/D19.22 gates, never inferred from browser user roles.
	authority := func(ctx context.Context, tx *sql.Tx, tenant uuid.UUID, write bool) error {
		if tenant != f.tenant {
			return ErrDecisionAuthorizationDenied
		}
		var one int
		return tx.QueryRowContext(ctx, "SELECT 1").Scan(&one)
	}
	owner, err := mysqlpublication.New(f.db, f.database, f.tenant, authority)
	if err != nil {
		t.Fatal(err)
	}
	sink, err := NewScreeningProjectionSink(f.store)
	if err != nil {
		t.Fatal(err)
	}
	projector, err := mysqlpublication.NewProjector(owner, f.database, sink)
	if err != nil {
		t.Fatal(err)
	}
	check := func(wantVersion int64, wantStatus string, wantMatches int) {
		t.Helper()
		var version int64
		var status string
		var matches int
		if err := f.db.QueryRowContext(ctx, "SELECT projection_version,status,match_count FROM "+schema+".screening_screenings WHERE tenant_id=? AND id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(source)).Scan(&version, &status, &matches); err != nil || version != wantVersion || status != wantStatus || matches != wantMatches {
			t.Fatal("source state", version, status, matches, err)
		}
	}
	version, err := owner.Start(ctx, source, 1)
	if err != nil || version != 2 {
		t.Fatal(version, err)
	}
	if _, err := owner.Fail(ctx, source, 1, "stale"); !errors.Is(err, mysqlpublication.ErrConflict) {
		t.Fatal("stale writer", err)
	}
	version, err = owner.Fail(ctx, source, version, "provider unavailable")
	if err != nil {
		t.Fatal(err)
	}
	version, err = owner.Retry(ctx, source, version)
	if err != nil {
		t.Fatal(err)
	}
	version, err = owner.Start(ctx, source, version)
	if err != nil {
		t.Fatal(err)
	}
	first, second := uuid.New(), uuid.New()
	result := mysqlpublication.Result{Response: []byte(`{"private":"not projected"}`), Reference: "accepted", Matches: []mysqlpublication.Match{{ID: first, EntityID: "one", Provider: "fixture", Name: "First", Status: "pending", Payload: []byte(`{}`)}, {ID: second, EntityID: "two", Provider: "fixture", Name: "Second", Status: "pending", Payload: []byte(`{}`)}}}
	t.Run("required_delivery_failure_rolls_back_matches", func(t *testing.T) {
		exec("CREATE TRIGGER " + schema + ".reject_screening_queue BEFORE UPDATE ON " + schema + ".screening_execution_projection_queue FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='required queue failure'")
		if _, err := owner.Complete(ctx, source, version, result); err == nil {
			t.Fatal("queue failure hidden")
		}
		exec("DROP TRIGGER " + schema + ".reject_screening_queue")
		check(version, "processing", 0)
		var n int
		if err := f.db.QueryRowContext(ctx, "SELECT count(*) FROM "+schema+".screening_matches").Scan(&n); err != nil || n != 0 {
			t.Fatal("partial evidence", n, err)
		}
	})
	version, err = owner.Complete(ctx, source, version, result)
	if err != nil {
		t.Fatal(err)
	}
	check(version, "awaiting_review", 2)
	t.Run("review_concurrency_and_rebinding", func(t *testing.T) {
		results := make(chan error, 2)
		for _, match := range []uuid.UUID{first, second} {
			go func(id uuid.UUID) { _, err := owner.Review(ctx, source, id, version, "confirmed_hit"); results <- err }(match)
		}
		wins, stale := 0, 0
		for range 2 {
			e := <-results
			if e == nil {
				wins++
			} else if errors.Is(e, mysqlpublication.ErrConflict) {
				stale++
			} else {
				t.Fatal(e)
			}
		}
		if wins != 1 || stale != 1 {
			t.Fatal(wins, stale)
		}
		version++
		if _, err := owner.Review(ctx, source, uuid.New(), version, "no_hit"); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal("foreign match", err)
		}
	})
	for _, match := range []uuid.UUID{first, second} {
		version, err = owner.Review(ctx, source, match, version, "no_hit")
		if err != nil {
			t.Fatal(err)
		}
	}
	check(version, "no_hit", 2)
	t.Run("consumer_failure_retries_without_partial_projection", func(t *testing.T) {
		exec("CREATE TRIGGER " + schema + ".reject_screening_projection BEFORE UPDATE ON " + schema + ".decision_engine_screening_executions FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='projection failure'")
		out, err := projector.RunBatch(ctx, 10)
		if err != nil || out.Retried != 1 || out.Applied != 0 {
			t.Fatal(out, err)
		}
		var attempts int
		var code string
		if err := f.db.QueryRowContext(ctx, "SELECT attempts,last_error FROM "+schema+".screening_execution_projection_queue WHERE tenant_id=? AND screening_id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(source)).Scan(&attempts, &code); err != nil || attempts != 1 || code != "projection_rejected" {
			t.Fatal(attempts, code, err)
		}
		exec("DROP TRIGGER " + schema + ".reject_screening_projection")
	})
	version, err = owner.Archive(ctx, source, version)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("newest_source_drains_once", func(t *testing.T) {
		out, err := projector.RunBatch(ctx, 10)
		if err != nil || out.Applied != 1 || out.Retried != 0 {
			t.Fatal(out, err)
		}
		out, err = projector.RunBatch(ctx, 10)
		if err != nil || out != (mysqlpublication.BatchResult{}) {
			t.Fatal("duplicate drain", out, err)
		}
		var projected int64
		if err := f.db.QueryRowContext(ctx, "SELECT projection_source_version FROM "+schema+".decision_engine_screening_executions WHERE tenant_id=? AND id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(uuid.MustParse(execution.ID))).Scan(&projected); err != nil || projected != version {
			t.Fatal("projection regressed", projected, version, err)
		}
		// A retained older queue version must reconcile current source and must
		// never regress the target or delete a different current queue version.
		exec("INSERT INTO "+schema+".screening_execution_projection_queue(tenant_id,screening_id,version,available_at,updated_at) VALUES(?,?,1,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))", mysqlstore.UUID(f.tenant), mysqlstore.UUID(source))
		out, err = projector.RunBatch(ctx, 1)
		if err != nil || out.Applied != 1 {
			t.Fatal(out, err)
		}
		if err := f.db.QueryRowContext(ctx, "SELECT projection_source_version FROM "+schema+".decision_engine_screening_executions WHERE id=?", mysqlstore.UUID(uuid.MustParse(execution.ID))).Scan(&projected); err != nil || projected != version {
			t.Fatal("old delivery regressed target", projected, err)
		}
	})
	if _, err := owner.Retry(ctx, source, version); !errors.Is(err, mysqlpublication.ErrConflict) {
		t.Fatal("archived retry", err)
	}
	foreign, err := mysqlpublication.New(f.db, f.database, uuid.New(), authority)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := foreign.Start(ctx, source, version); !errors.Is(err, ErrDecisionAuthorizationDenied) {
		t.Fatal("foreign source", err)
	}
	if _, err := mysqlpublication.NewProjector(owner, "foreign", sink); err == nil {
		t.Fatal("foreign projector")
	}
}
