package postgres

import (
	"context"
	"errors"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"time"
)

func (m Maintenance) AssignWaiting(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 1000 {
		return 0, casepkg.Invalid("batch limit must be 1..1000")
	}
	count := 0
	for attempt := 0; attempt < limit; attempt++ {
		tx, err := m.DB.Begin(ctx)
		if err != nil {
			return count, err
		}
		changed, err := assignOne(ctx, tx)
		if err == nil {
			err = tx.Commit(ctx)
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		_ = tx.Rollback(cleanup)
		cancel()
		var constraint *pgconn.PgError
		if errors.As(err, &constraint) && constraint.ConstraintName == "assignment_capacity" {
			continue
		}
		if err != nil {
			return count, err
		}
		if !changed {
			break
		}
		count++
	}
	return count, nil
}

func assignOne(ctx context.Context, tx pgx.Tx) (bool, error) {
	var tenant, inbox, id uuid.UUID
	err := tx.QueryRow(ctx, `WITH eligible_inboxes AS MATERIALIZED (
 SELECT DISTINCT u.tenant_id,u.inbox_id FROM case_manager.inbox_users u
 JOIN case_manager.inboxes i ON i.tenant_id=u.tenant_id AND i.id=u.inbox_id
 WHERE i.status='active' AND i.auto_assign_enabled AND u.auto_assign_enabled
 AND u.capacity>(SELECT count(*) FROM case_manager.cases a WHERE a.tenant_id=u.tenant_id AND a.inbox_id=u.inbox_id AND a.assigned_to=u.user_id AND a.status<>'closed'))
 SELECT c.tenant_id,c.inbox_id,c.id FROM case_manager.cases c
 JOIN eligible_inboxes i ON i.tenant_id=c.tenant_id AND i.inbox_id=c.inbox_id
 WHERE c.status<>'closed' AND c.assigned_to IS NULL AND (c.snoozed_until IS NULL OR c.snoozed_until<=now())
 ORDER BY c.created_at,c.id LIMIT 1 FOR UPDATE OF c SKIP LOCKED`).Scan(&tenant, &inbox, &id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var enabled bool
	if err := tx.QueryRow(ctx, `SELECT status='active' AND auto_assign_enabled FROM case_manager.inboxes WHERE tenant_id=$1 AND id=$2 FOR SHARE`, tenant, inbox).Scan(&enabled); err != nil {
		return false, err
	}
	if !enabled {
		return false, nil
	}
	var user string
	err = tx.QueryRow(ctx, `SELECT u.user_id FROM case_manager.inbox_users u
 WHERE u.tenant_id=$1 AND u.inbox_id=$2 AND u.auto_assign_enabled
 AND u.capacity>(SELECT count(*) FROM case_manager.cases c WHERE c.tenant_id=u.tenant_id AND c.inbox_id=u.inbox_id AND c.assigned_to=u.user_id AND c.status<>'closed')
 ORDER BY (SELECT count(*) FROM case_manager.cases c WHERE c.tenant_id=u.tenant_id AND c.inbox_id=u.inbox_id AND c.assigned_to=u.user_id AND c.status<>'closed'),u.user_id
 LIMIT 1 FOR UPDATE OF u SKIP LOCKED`, tenant, inbox).Scan(&user)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `UPDATE case_manager.cases SET assigned_to=$3,updated_at=$4 WHERE tenant_id=$1 AND id=$2`, tenant, id, user, now); err != nil {
		return false, err
	}
	_, err = NewEventRepository(tx).Create(ctx, casepkg.Event{ID: uuid.New(), TenantID: tenant, CaseID: id, EventType: "case_auto_assigned", NewValue: user, CreatedAt: now})
	return err == nil, err
}
