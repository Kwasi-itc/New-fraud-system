package mysqlpublication

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// ProjectionSink belongs to the decision owner. Prepare authorizes on the local
// transaction before any source locks; its closure may perform local SQL only.
type ProjectionSink interface {
	Matches(*sql.DB, string, uuid.UUID) bool
	Prepare(context.Context, *sql.Tx, uuid.UUID) (func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error, error)
}
type Projector struct {
	store *Store
	sink  ProjectionSink
}
type BatchResult struct{ Applied, Retried int }

func NewProjector(store *Store, database string, sink ProjectionSink) (*Projector, error) {
	if store == nil || sink == nil || !sink.Matches(store.db, database, store.tenant) {
		return nil, ErrInvalid
	}
	schema, err := mysqlstore.Identifier(database)
	if err != nil || schema != store.schema {
		return nil, ErrInvalid
	}
	return &Projector{store, sink}, nil
}
func (p *Projector) RunBatch(ctx context.Context, limit int) (BatchResult, error) {
	var out BatchResult
	if p == nil || limit < 1 || limit > 1000 {
		return out, ErrInvalid
	}
	for range limit {
		found, retry, err := p.one(ctx)
		if err != nil {
			return out, err
		}
		if !found {
			return out, nil
		}
		if retry {
			out.Retried++
		} else {
			out.Applied++
		}
	}
	return out, nil
}
func (p *Projector) one(ctx context.Context) (found, retry bool, err error) {
	s := p.store
	err = mysqlstore.Mutation(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.authorize(ctx, tx, s.tenant, true); err != nil {
			return err
		}
		apply, err := p.sink.Prepare(ctx, tx, s.tenant)
		if err != nil {
			return err
		}
		// Bounded due-queue candidates, then source -> queue -> projection locks.
		// This avoids acquiring the queue first through an optimizer-chosen join.
		rows, err := tx.QueryContext(ctx, "SELECT screening_id FROM "+s.schema+".screening_execution_projection_queue WHERE tenant_id=? AND available_at<=UTC_TIMESTAMP(6) ORDER BY available_at,screening_id LIMIT 64", mysqlstore.UUID(s.tenant))
		if err != nil {
			return err
		}
		var candidates []mysqlstore.UUID
		for rows.Next() {
			var id mysqlstore.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			candidates = append(candidates, id)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		for _, candidate := range candidates {
			var decision, config mysqlstore.UUID
			err := tx.QueryRowContext(ctx, "SELECT decision_id,screening_config_id FROM "+s.schema+".screening_screenings WHERE tenant_id=? AND id=? AND execution_id IS NOT NULL FOR UPDATE SKIP LOCKED", mysqlstore.UUID(s.tenant), candidate).Scan(&decision, &config)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			var version int64
			var attempts int
			err = tx.QueryRowContext(ctx, "SELECT version,attempts FROM "+s.schema+".screening_execution_projection_queue WHERE tenant_id=? AND screening_id=? AND available_at<=UTC_TIMESTAMP(6) FOR UPDATE", mysqlstore.UUID(s.tenant), candidate).Scan(&version, &attempts)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			found = true
			if _, err := tx.ExecContext(ctx, "SAVEPOINT screening_projection"); err != nil {
				return err
			}
			if err := apply(ctx, uuid.UUID(candidate), uuid.UUID(decision), uuid.UUID(config)); err != nil {
				if _, rollback := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT screening_projection"); rollback != nil {
					return errors.Join(err, rollback)
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				retry = true
				_, err = tx.ExecContext(ctx, "UPDATE "+s.schema+".screening_execution_projection_queue SET attempts=LEAST(attempts+1,1000000),last_error='projection_rejected',available_at=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6)),updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND screening_id=? AND version=?", RetryDelay(attempts+1).Microseconds(), mysqlstore.UUID(s.tenant), candidate, version)
				return err
			}
			_, err = tx.ExecContext(ctx, "DELETE FROM "+s.schema+".screening_execution_projection_queue WHERE tenant_id=? AND screening_id=? AND version=?", mysqlstore.UUID(s.tenant), candidate, version)
			return err
		}
		return nil
	})
	return found, retry, err
}
