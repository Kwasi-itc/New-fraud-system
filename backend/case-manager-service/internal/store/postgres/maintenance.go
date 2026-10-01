package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Maintenance holds no database locks while delivering to external consumers.
type Maintenance struct{ DB *pgxpool.Pool }

func (m Maintenance) WakeSnoozed(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 1000 {
		return 0, casepkg.Invalid("batch limit must be 1..1000")
	}
	completed := 0
	for completed < limit {
		tx, err := m.DB.Begin(ctx)
		if err != nil {
			return completed, err
		}
		changed, err := wakeOne(ctx, tx)
		if err == nil {
			err = tx.Commit(ctx)
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		_ = tx.Rollback(cleanup)
		cancel()
		if err != nil {
			return completed, err
		}
		if !changed {
			break
		}
		completed++
	}
	return completed, nil
}

func wakeOne(ctx context.Context, tx pgx.Tx) (bool, error) {
	var tenant, id uuid.UUID
	var previous time.Time
	err := tx.QueryRow(ctx, `SELECT tenant_id,id,snoozed_until FROM case_manager.cases
 WHERE status<>'closed' AND snoozed_until<=now()
 ORDER BY snoozed_until,id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&tenant, &id, &previous)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `UPDATE case_manager.cases SET snoozed_until=NULL,boost_reason='unsnoozed',updated_at=$3 WHERE tenant_id=$1 AND id=$2`, tenant, id, now)
	if err != nil {
		return false, err
	}
	_, err = NewEventRepository(tx).Create(ctx, casepkg.Event{ID: uuid.New(), TenantID: tenant, CaseID: id, EventType: "snooze_expired", PreviousValue: previous.UTC().Format(time.RFC3339Nano), CreatedAt: now})
	return err == nil, err
}

type Delivery struct {
	ID            uuid.UUID       `json:"id"`
	TenantID      uuid.UUID       `json:"tenant_id"`
	AggregateType string          `json:"aggregate_type"`
	AggregateID   string          `json:"aggregate_id"`
	EventType     string          `json:"event_type"`
	Payload       json.RawMessage `json:"payload"`
	CreatedAt     time.Time       `json:"created_at"`
	Attempt       int             `json:"-"`
	Token         uuid.UUID       `json:"-"`
}

func (m Maintenance) ClaimDelivery(ctx context.Context) (*Delivery, error) {
	d := Delivery{Token: uuid.New()}
	err := m.DB.QueryRow(ctx, `WITH candidate AS (
 SELECT id FROM case_manager.outbox_events
 WHERE status IN ('pending','delivering') AND next_attempt_at<=now()
 AND (lease_until IS NULL OR lease_until<=now())
 ORDER BY next_attempt_at,created_at,id LIMIT 1 FOR UPDATE SKIP LOCKED)
 UPDATE case_manager.outbox_events e SET status='delivering',lease_token=$1,
 lease_until=now()+interval '60 seconds',attempts=e.attempts+1
 FROM candidate c WHERE e.id=c.id
 RETURNING e.id,e.tenant_id,e.aggregate_type,e.aggregate_id,e.event_type,e.payload,e.created_at,e.attempts`, d.Token).Scan(&d.ID, &d.TenantID, &d.AggregateType, &d.AggregateID, &d.EventType, &d.Payload, &d.CreatedAt, &d.Attempt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (m Maintenance) FinishDelivery(ctx context.Context, d Delivery, failure string) error {
	status := "delivered"
	delay := time.Duration(1<<min(d.Attempt, 12)) * time.Second
	if failure != "" {
		status = "pending"
		if d.Attempt >= 12 {
			status = "failed"
		}
	}
	result, err := m.DB.Exec(ctx, `UPDATE case_manager.outbox_events SET status=$3,
 next_attempt_at=now()+$4::double precision*interval '1 second',lease_token=NULL,lease_until=NULL,
 last_error=NULLIF($5,''),delivered_at=CASE WHEN $3='delivered' THEN now() ELSE NULL END
 WHERE id=$1 AND lease_token=$2 AND status='delivering'`, d.ID, d.Token, status, delay.Seconds(), failure)
	if err == nil && result.RowsAffected() != 1 {
		return casepkg.ErrConflict
	}
	return err
}

// Replay retains the event identity and never alters the persisted case history.
func (m Maintenance) Replay(ctx context.Context, tenant, id uuid.UUID) error {
	result, err := m.DB.Exec(ctx, `UPDATE case_manager.outbox_events SET status='pending',attempts=0,
 next_attempt_at=now(),last_error=NULL,delivered_at=NULL WHERE tenant_id=$1 AND id=$2 AND status IN ('failed','delivered')`, tenant, id)
	if err == nil && result.RowsAffected() != 1 {
		return casepkg.ErrConflict
	}
	return err
}
