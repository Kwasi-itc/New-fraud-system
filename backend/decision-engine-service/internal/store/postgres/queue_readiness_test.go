package postgres

import (
	"context"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/riverjobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/testsupport"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

func queueReadinessFixture(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	dsn := testsupport.PostgresDatabaseURL(t)
	db, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db, dsn
}

func installReadinessQueue(t *testing.T, db *pgxpool.Pool) {
	t.Helper()
	m, err := rivermigrate.New(riverpgxv5.New(db), &rivermigrate.Config{Schema: riverjobs.Schema})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(context.Background(), rivermigrate.DirectionUp, nil); err != nil {
		t.Fatal(err)
	}
}

func TestDecisionQueueReadinessRequiresPinnedSchema(t *testing.T) {
	ctx := context.Background()
	if err := ValidateQueueReadiness(ctx, nil, false); err == nil {
		t.Fatal("nil database reported ready")
	}
	db, dsn := queueReadinessFixture(t)
	if err := ValidateQueueReadiness(ctx, db, false); err == nil {
		t.Fatal("empty database reported queue readiness")
	}
	installReadinessQueue(t, db)
	for _, worker := range []bool{false, true} {
		if err := ValidateQueueReadiness(ctx, db, worker); err != nil {
			t.Fatalf("installed queue worker=%t: %v", worker, err)
		}
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = "pg_catalog"
	otherPath, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer otherPath.Close()
	if err := ValidateQueueReadiness(ctx, otherPath, true); err != nil {
		t.Fatalf("explicit public queue depended on ambient search_path: %v", err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO public.river_migration(line,version) VALUES('main',999)`); err != nil {
		t.Fatal(err)
	}
	if err := ValidateQueueReadiness(ctx, db, false); err == nil {
		t.Fatal("unknown future lineage reported ready")
	}
	if _, err := db.Exec(ctx, `DELETE FROM public.river_migration WHERE version=999; DROP INDEX public.river_job_unique_idx`); err != nil {
		t.Fatal(err)
	}
	if err := ValidateQueueReadiness(ctx, db, false); err == nil {
		t.Fatal("missing unique queue index reported ready")
	}
}

func TestDecisionQueueReadinessSeparatesProducerAndWorkerAccess(t *testing.T) {
	ctx := context.Background()
	db, dsn := queueReadinessFixture(t)
	installReadinessQueue(t, db)
	// A generated role in the private fixture; never revoke production grants.
	role := pgx.Identifier{"decision_fixture_" + uuid.NewString()}.Sanitize()
	if _, err := db.Exec(ctx, "CREATE ROLE "+role); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(ctx, "DROP OWNED BY "+role+"; DROP ROLE "+role); err != nil {
			t.Errorf("cleanup fixture role: %v", err)
		}
	})
	if _, err := db.Exec(ctx, "GRANT USAGE ON SCHEMA public TO "+role+"; GRANT SELECT,INSERT,UPDATE ON public.river_job TO "+role+"; GRANT SELECT ON public.river_migration TO "+role+"; GRANT USAGE ON SEQUENCE public.river_job_id_seq TO "+role); err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, err := c.Exec(ctx, "SET ROLE "+role)
		return err
	}
	restricted, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restricted.Close)
	if err := ValidateQueueReadiness(ctx, restricted, false); err != nil {
		t.Fatalf("least-privilege producer: %v", err)
	}
	if err := ValidateQueueReadiness(ctx, restricted, true); err == nil {
		t.Fatal("producer-only role reported worker readiness")
	}
	if _, err := db.Exec(ctx, "GRANT SELECT,INSERT,UPDATE,DELETE ON public.river_job,public.river_queue,public.river_leader,public.river_client,public.river_client_queue TO "+role); err != nil {
		t.Fatal(err)
	}
	if err := ValidateQueueReadiness(ctx, restricted, true); err != nil {
		t.Fatalf("worker access: %v", err)
	}
	if _, err := db.Exec(ctx, "REVOKE USAGE ON SEQUENCE public.river_job_id_seq FROM "+role); err != nil {
		t.Fatal(err)
	}
	if err := ValidateQueueReadiness(ctx, restricted, false); err == nil {
		t.Fatal("producer without sequence access reported readiness")
	}
}
