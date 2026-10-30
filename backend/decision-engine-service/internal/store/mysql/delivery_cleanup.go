package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
	"github.com/google/uuid"
)

type DeliveryCleanupCursor struct {
	FinishedAt time.Time
	ID         uuid.UUID
}

func deliveryRetention(state workers.DeliveryState) (time.Duration, error) {
	var out workers.DeliveryOutcome
	var err error
	switch state {
	case workers.Completed:
		out, err = workers.ResolveDeliveryOutcome(1, 1, false)
	case workers.Discarded:
		out, err = workers.ResolveDeliveryOutcome(1, 1, true)
	case workers.Cancelled:
		out, _, err = workers.ResolveCancellation(workers.Cancelled)
	default:
		return 0, errors.New("only completed/cancelled/discarded transport can expire")
	}
	if err != nil {
		return 0, err
	}
	if out.State != state || out.Retention <= 0 {
		return 0, errors.New("shared terminal retention policy mismatch")
	}
	return out.Retention, nil
}

// CleanupTerminal deletes transport ONLY, not owners or permanent receipts.
// Its indexed discovery is one bounded state stream. Receipt locks precede
// delivery locks, matching finish/cancel/rescue; SKIP LOCKED leaves busy rows
// for a later bounded cycle rather than waiting while holding unrelated locks.
// Required installation: the full execution/maintenance lineage and grants.
// Commit uncertainty returns zero plus error, not a claimed deletion count.
func (s *EffectDeliveryStore) CleanupTerminal(ctx context.Context, state workers.DeliveryState, limit int) (int, error) {
	n, _, err := s.CleanupTerminalPage(ctx, state, nil, limit)
	return n, err
}

// A page cursor advances over discovery, INCLUDING locked/skipped rows. Use
// paced cycles that wrap after nil; zero deletions does not imply exhaustion.
// This avoids one busy first page starving unrelated terminal resources.
func (s *EffectDeliveryStore) CleanupTerminalPage(ctx context.Context, state workers.DeliveryState, after *DeliveryCleanupCursor, limit int) (int, *DeliveryCleanupCursor, error) {
	retention, err := deliveryRetention(state)
	if err != nil {
		return 0, nil, err
	}
	if s == nil || s.db == nil || s.authorize == nil || limit < 1 || limit > 100 {
		return 0, nil, errors.New("scoped cleanup authority and terminal batch 1..100 required")
	}
	if after != nil && (after.ID == uuid.Nil || !validDeliveryDate(after.FinishedAt)) {
		return 0, nil, errors.New("invalid terminal cleanup cursor")
	}
	changed := 0
	var next *DeliveryCleanupCursor
	err = mysqlstore.Mutation(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.authorize(ctx, tx, s.tenant, s.kind, EffectWorkerCleanup); err != nil {
			return err
		}
		base := " WHERE tenant_id=? AND kind=? AND queue_name=? AND state=? AND finished_at<=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6))"
		args := []any{mysqlstore.UUID(s.tenant), string(s.kind), s.queue, string(state), -retention.Microseconds()}
		where, scanArgs := base, append([]any{}, args...)
		if after != nil {
			where += " AND(finished_at>? OR(finished_at=? AND id>?))"
			at := mysqlstore.DateTime{Time: after.FinishedAt}
			scanArgs = append(scanArgs, at, at, mysqlstore.UUID(after.ID))
		}
		rows, err := tx.QueryContext(ctx, "SELECT id,finished_at FROM "+s.schema+"."+s.deliveryTable()+where+" ORDER BY finished_at,id LIMIT ?", append(scanArgs, limit)...)
		if err != nil {
			return err
		}
		var ids []mysqlstore.UUID
		for rows.Next() {
			var id mysqlstore.UUID
			var at mysqlstore.DateTime
			if err := rows.Scan(&id, &at); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
			next = &DeliveryCleanupCursor{at.Time, uuid.UUID(id)}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(ids) < limit {
			next = nil
		}
		if len(ids) == 0 {
			return nil
		}
		marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
		receiptArgs := []any{mysqlstore.UUID(s.tenant), string(s.kind)}
		for _, id := range ids {
			receiptArgs = append(receiptArgs, id)
		}
		// MySQL 8.4 supports FOR SHARE SKIP LOCKED. Lock the immutable parent
		// first; acquiring delivery before receipt could deadlock a stale finish.
		rows, err = tx.QueryContext(ctx, "SELECT id FROM "+s.schema+"."+s.receiptTable()+" WHERE tenant_id=? AND kind=? AND id IN("+marks+") ORDER BY id FOR SHARE SKIP LOCKED", receiptArgs...)
		if err != nil {
			return err
		}
		ids, err = readDeliveryIDs(rows)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		marks = strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
		lockedArgs := append([]any{}, args...)
		for _, id := range ids {
			lockedArgs = append(lockedArgs, id)
		}
		rows, err = tx.QueryContext(ctx, "SELECT id FROM "+s.schema+"."+s.deliveryTable()+base+" AND id IN("+marks+") ORDER BY id FOR UPDATE SKIP LOCKED", lockedArgs...)
		if err != nil {
			return err
		}
		ids, err = readDeliveryIDs(rows)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		marks = strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
		deleteArgs := append([]any{}, args...)
		for _, id := range ids {
			deleteArgs = append(deleteArgs, id)
		}
		result, err := tx.ExecContext(ctx, "DELETE FROM "+s.schema+"."+s.deliveryTable()+base+" AND id IN("+marks+")", deleteArgs...)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != int64(len(ids)) {
			return errors.New("terminal cleanup changed eligibility while locked; reconcile database clock")
		}
		changed = int(n)
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	return changed, next, nil
}

func readDeliveryIDs(rows *sql.Rows) (out []mysqlstore.UUID, err error) {
	defer rows.Close()
	for rows.Next() {
		var id mysqlstore.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *ExecutionDeliveryStore) CleanupTerminal(ctx context.Context, state workers.DeliveryState, limit int) (int, error) {
	if s == nil || s.transport == nil {
		return 0, errors.New("initialized execution transport required")
	}
	return s.transport.CleanupTerminal(ctx, state, limit)
}

func (s *ExecutionDeliveryStore) CleanupTerminalPage(ctx context.Context, state workers.DeliveryState, after *DeliveryCleanupCursor, limit int) (int, *DeliveryCleanupCursor, error) {
	if s == nil || s.transport == nil {
		return 0, nil, errors.New("initialized execution transport required")
	}
	return s.transport.CleanupTerminalPage(ctx, state, after, limit)
}
