package postgres_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This is bounded shared-schema database contention, not a production capacity
// benchmark or an HTTP load test of independently deployed services.
func TestPostgresConcurrentSharedSchemaLoad(t *testing.T) {
	db := testDatabase(t, true)
	_, tenant, inbox, _ := fixture(t, db)
	decision := seedDecision(t, db, tenant)
	screening, _, _ := seedScreening(t, db, tenant, decision)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	owners := []string{"data-model", "ingestion", "decision-engine", "screening", "case-manager"}
	pools := make([]*pgxpool.Pool, len(owners))
	for i, owner := range owners {
		cfg := db.Config()
		cfg.MaxConns, cfg.MinConns = 2, 0
		cfg.ConnConfig.RuntimeParams["application_name"] = "phase1-test-" + owner
		cfg.ConnConfig.RuntimeParams["lock_timeout"] = "1000"
		cfg.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
		pool, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Close()
		pools[i] = pool
	}
	const workers, commands = 4, 15
	start := make(chan struct{})
	errors := make(chan error, len(owners)*workers)
	var wg sync.WaitGroup
	started := time.Now()
	for i, owner := range owners {
		for worker := 0; worker < workers; worker++ {
			wg.Add(1)
			go func(i, worker int, owner string) {
				defer wg.Done()
				<-start
				pool := pools[i]
				for n := 0; n < commands; n++ {
					var err error
					switch owner {
					case "data-model":
						_, err = pool.Exec(ctx, `UPDATE core.tenants SET updated_at=now() WHERE id=$1`, tenant)
					case "ingestion":
						_, err = pool.Exec(ctx, `INSERT INTO core_ingestion.outbox_events(id,tenant_id,event_type,aggregate_type,aggregate_key,created_at)
						VALUES(gen_random_uuid(),$1,'record.ingested','transaction','phase1-load',now())`, tenant)
					case "decision-engine":
						_, err = pool.Exec(ctx, `UPDATE core.decisions SET score=score+1 WHERE tenant_id=$1 AND id=$2`, tenant, decision)
					case "screening":
						_, err = pool.Exec(ctx, `UPDATE screening.screenings SET updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenant, screening)
					case "case-manager":
						s := newService(pool)
						_, err = s.CreateCase(serviceContext(tenant), service.CreateCaseInput{
							TenantID: tenant, InboxID: inbox.ID, Name: fmt.Sprintf("Load %d/%d", worker, n), DecisionIDs: []uuid.UUID{decision},
						}, nil)
					}
					if err != nil {
						errors <- fmt.Errorf("%s command %d: %w", owner, n, err)
						return
					}
				}
			}(i, worker, owner)
		}
	}
	close(start)
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	for i, pool := range pools {
		stat := pool.Stat()
		if stat.TotalConns() > 2 || stat.CanceledAcquireCount() != 0 {
			t.Errorf("%s exceeded pool budget or timed out: connections=%d canceled=%d", owners[i], stat.TotalConns(), stat.CanceledAcquireCount())
		}
		t.Logf("owner=%s max_connections=%d acquired=%d pool_waits=%d acquire_time=%s", owners[i], stat.MaxConns(), stat.AcquiredConns(), stat.EmptyAcquireCount(), stat.AcquireDuration())
	}
	if got := count(t, db, "cases"); got != 1+workers*commands {
		t.Fatalf("lost case writes under shared load: %d", got)
	}
	var score int
	if err := db.QueryRow(ctx, `SELECT score FROM core.decisions WHERE tenant_id=$1 AND id=$2`, tenant, decision).Scan(&score); err != nil || score != workers*commands {
		t.Fatalf("lost decision updates under shared load: %d %v", score, err)
	}
	t.Logf("shared load: %d commands across %d owner pools in %s; workload connection ceiling=%d", len(owners)*workers*commands, len(owners), time.Since(started), 2*len(owners))
}
