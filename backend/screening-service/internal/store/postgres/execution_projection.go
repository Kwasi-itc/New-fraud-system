package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ExecutionProjector drains source-owned work through a core-owned mutation
// contract. No network IO or long-lived delivery lease is needed in a shared DB.
type ExecutionProjector struct{ db *pgxpool.Pool }

func NewExecutionProjector(db *pgxpool.Pool) ExecutionProjector { return ExecutionProjector{db} }
func (p ExecutionProjector) RunBatch(ctx context.Context, limit int) (failed int, err error) {
	if limit < 1 || limit > 1000 {
		return 0, fmt.Errorf("projection batch limit must be 1..1000")
	}
	for i := 0; i < limit; i++ {
		step, cancel := context.WithTimeout(ctx, 5*time.Second)
		found, retry, e := p.projectOne(step)
		cancel()
		if e != nil {
			return failed, e
		}
		if !found {
			return failed, nil
		}
		if retry {
			failed++
		}
	}
	return failed, nil
}
func (p ExecutionProjector) projectOne(ctx context.Context) (found, retry bool, err error) {
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return false, false, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	var tenant, source string
	// Producers lock source then queue. Match that order, skipping busy sources;
	// holding the queue first would deadlock a concurrent source update.
	err = tx.QueryRow(ctx, `SELECT s.tenant_id::text,s.id::text FROM screening.execution_projection_queue q JOIN screening.screenings s ON s.tenant_id=q.tenant_id AND s.id=q.screening_id WHERE q.available_at<=now() ORDER BY q.available_at,q.tenant_id,q.screening_id LIMIT 1 FOR UPDATE OF s SKIP LOCKED`).Scan(&tenant, &source)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	// Recheck after acquiring source authority; another consumer may have drained it.
	var version int64
	err = tx.QueryRow(ctx, `SELECT version FROM screening.execution_projection_queue WHERE tenant_id=$1 AND screening_id=$2 AND available_at<=now() FOR UPDATE`, tenant, source).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, false, tx.Commit(ctx)
	}
	if err != nil {
		return true, false, err
	}
	save, err := tx.Begin(ctx)
	if err != nil {
		return true, false, err
	}
	_, projectionErr := save.Exec(ctx, `SELECT core.reconcile_screening_source($1,$2)`, tenant, source)
	if projectionErr != nil {
		if err = save.Rollback(ctx); err != nil {
			return true, false, err
		}
		// Retry indefinitely with a capped delay; expose failures for operator repair.
		_, err = tx.Exec(ctx, `UPDATE screening.execution_projection_queue SET attempts=least(attempts+1,1000000),last_error=left($3,2000),available_at=now()+make_interval(secs=>least(300,power(2,least(attempts+1,8)))::integer),updated_at=now() WHERE tenant_id=$1 AND screening_id=$2`, tenant, source, projectionErr.Error())
		if err != nil {
			return true, false, err
		}
		return true, true, tx.Commit(ctx)
	}
	if err = save.Commit(ctx); err != nil {
		return true, false, err
	}
	_, err = tx.Exec(ctx, `DELETE FROM screening.execution_projection_queue WHERE tenant_id=$1 AND screening_id=$2 AND version=$3`, tenant, source, version)
	if err != nil {
		return true, false, err
	}
	return true, false, tx.Commit(ctx)
}
