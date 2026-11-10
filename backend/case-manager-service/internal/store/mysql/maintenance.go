package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	c "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
)

// Maintenance claims individual records, never a tenant-wide parent lock.
// Network publication happens only after ClaimDelivery commits.
type Maintenance struct {
	DB       *sql.DB
	Database string
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
	inbox         bool
}

func (m Maintenance) transaction(ctx context.Context, fn func(scope) (bool, error)) (bool, error) {
	tx, e := m.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return false, e
	}
	defer tx.Rollback()
	repos, e := NewRepositories(tx, m.Database)
	if e != nil {
		return false, e
	}
	s := repos.Cases.(caseRepo).scope
	changed, e := fn(s)
	if e != nil {
		return false, e
	}
	return changed, tx.Commit()
}
func (m Maintenance) CleanUploads(ctx context.Context, n int) error {
	if n < 1 || n > 1000 {
		return c.Invalid("batch limit must be 1..1000")
	}
	_, e := m.transaction(ctx, func(s scope) (bool, error) {
		rows, e := s.db.QueryContext(ctx, "SELECT id FROM "+s.table("evidence_uploads")+" WHERE finalized=FALSE AND expires_at<=CURRENT_TIMESTAMP(6) ORDER BY expires_at,id LIMIT ? FOR UPDATE SKIP LOCKED", n)
		if e != nil {
			return false, e
		}
		var ids []uuid.UUID
		for rows.Next() {
			var u uuid.UUID
			if e = rows.Scan(uid(&u)); e != nil {
				rows.Close()
				return false, e
			}
			ids = append(ids, u)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return false, e
		}
		for _, u := range ids {
			if _, e = s.db.ExecContext(ctx, "DELETE FROM "+s.table("evidence_uploads")+" WHERE id=? AND finalized=FALSE", id(u)); e != nil {
				return false, e
			}
		}
		return len(ids) > 0, nil
	})
	return e
}
func (m Maintenance) WakeSnoozed(ctx context.Context, n int) (int, error) {
	if n < 1 || n > 1000 {
		return 0, c.Invalid("batch limit must be 1..1000")
	}
	count := 0
	for count < n {
		ok, e := m.transaction(ctx, func(s scope) (bool, error) {
			var t, i uuid.UUID
			var previous time.Time
			e := s.db.QueryRowContext(ctx, "SELECT tenant_id,id,snoozed_until FROM "+s.table("cases")+" WHERE status<>'closed' AND snoozed_until<=CURRENT_TIMESTAMP(6) ORDER BY snoozed_until,id LIMIT 1 FOR UPDATE SKIP LOCKED").Scan(uid(&t), uid(&i), &previous)
			if errors.Is(e, sql.ErrNoRows) {
				return false, nil
			}
			if e != nil {
				return false, e
			}
			now := time.Now().UTC()
			_, e = s.db.ExecContext(ctx, "UPDATE "+s.table("cases")+" SET snoozed_until=NULL,updated_at=? WHERE tenant_id=? AND id=?", now, id(t), id(i))
			if e != nil {
				return false, e
			}
			_, e = (eventRepo{s}).Create(ctx, c.Event{ID: uuid.New(), TenantID: t, CaseID: i, EventType: "case_unsnoozed", PreviousValue: previous.UTC().Format(time.RFC3339), CreatedAt: now})
			return e == nil, e
		})
		if e != nil {
			return count, e
		}
		if !ok {
			break
		}
		count++
	}
	return count, nil
}
func (m Maintenance) AssignWaiting(ctx context.Context, n int) (int, error) {
	if n < 1 || n > 1000 {
		return 0, c.Invalid("batch limit must be 1..1000")
	}
	count := 0
	for attempt := 0; attempt < n; attempt++ {
		ok, e := m.transaction(ctx, func(s scope) (bool, error) {
			var t, i, key uuid.UUID
			// Select eligible inboxes before cases. An old case whose members have
			// been suspended must not stop assignment in other inboxes.
			query := `WITH eligible AS(SELECT DISTINCT u.tenant_id,u.inbox_id FROM ` + s.table("inbox_users") + ` u
 JOIN ` + s.table("inboxes") + ` i ON i.tenant_id=u.tenant_id AND i.id=u.inbox_id
 JOIN ` + s.schema + `.identity_memberships im ON im.tenant_id=u.tenant_id AND im.user_id=u.user_id
 JOIN ` + s.schema + `.identity_users iu ON iu.id=u.user_id
 JOIN ` + s.schema + `.identity_tenants it ON it.id=u.tenant_id
 WHERE i.active AND i.auto_assign_enabled AND u.auto_assign_enabled AND im.active AND iu.active AND it.active AND((im.roles+0)&6)<>0
 AND u.capacity>(SELECT COUNT(*) FROM ` + s.table("cases") + ` a WHERE a.tenant_id=u.tenant_id AND a.inbox_id=u.inbox_id AND a.assigned_to=BIN_TO_UUID(u.user_id) AND a.status<>'closed'))
 SELECT c.tenant_id,c.inbox_id,c.id FROM eligible i JOIN ` + s.table("cases") + ` c ON c.tenant_id=i.tenant_id AND c.inbox_id=i.inbox_id
 WHERE c.status<>'closed' AND c.assigned_to IS NULL AND(c.snoozed_until IS NULL OR c.snoozed_until<=CURRENT_TIMESTAMP(6))
 ORDER BY c.created_at,c.id LIMIT 1 FOR UPDATE OF c SKIP LOCKED`
			e := s.db.QueryRowContext(ctx, query).Scan(uid(&t), uid(&i), uid(&key))
			if errors.Is(e, sql.ErrNoRows) {
				return false, nil
			}
			if e != nil {
				return false, e
			}
			var enabled bool
			e = s.db.QueryRowContext(ctx, "SELECT active AND auto_assign_enabled FROM "+s.table("inboxes")+" WHERE tenant_id=? AND id=? FOR SHARE", id(t), id(i)).Scan(&enabled)
			if e != nil || !enabled {
				return false, e
			}
			var user uuid.UUID
			e = s.db.QueryRowContext(ctx, "SELECT u.user_id FROM "+s.table("inbox_users")+" u WHERE u.tenant_id=? AND u.inbox_id=? AND u.auto_assign_enabled AND EXISTS(SELECT 1 FROM "+s.schema+".identity_memberships m JOIN "+s.schema+".identity_users a ON a.id=m.user_id JOIN "+s.schema+".identity_tenants t ON t.id=m.tenant_id WHERE m.tenant_id=u.tenant_id AND m.user_id=u.user_id AND m.active AND a.active AND t.active AND((m.roles+0)&6)<>0) AND u.capacity>(SELECT COUNT(*) FROM "+s.table("cases")+" c WHERE c.tenant_id=u.tenant_id AND c.inbox_id=u.inbox_id AND c.assigned_to=BIN_TO_UUID(u.user_id) AND c.status<>'closed') ORDER BY(SELECT COUNT(*) FROM "+s.table("cases")+" c WHERE c.tenant_id=u.tenant_id AND c.inbox_id=u.inbox_id AND c.assigned_to=BIN_TO_UUID(u.user_id) AND c.status<>'closed'),u.user_id LIMIT 1 FOR UPDATE SKIP LOCKED", id(t), id(i)).Scan(uid(&user))
			if errors.Is(e, sql.ErrNoRows) {
				return false, nil
			}
			if e != nil {
				return false, e
			}
			now := time.Now().UTC()
			name := user.String()
			if e = (caseRepo{s}).Assign(ctx, t, key, &name, now); e != nil {
				return false, e
			}
			_, e = (eventRepo{s}).Create(ctx, c.Event{ID: uuid.New(), TenantID: t, CaseID: key, EventType: "case_auto_assigned", NewValue: name, CreatedAt: now})
			return e == nil, e
		})
		if e != nil {
			return count, e
		}
		if !ok {
			break
		}
		count++
	}
	return count, nil
}
func (m Maintenance) ClaimDelivery(ctx context.Context) (*Delivery, error) {
	var out *Delivery
	_, e := m.transaction(ctx, func(s scope) (bool, error) {
		for _, inbox := range []bool{false, true} {
			table := "outbox_events"
			if inbox {
				table = "inbox_outbox"
			}
			v := Delivery{Token: uuid.New(), inbox: inbox}
			var aggregate uuid.UUID
			e := s.db.QueryRowContext(ctx, "SELECT id,tenant_id,aggregate_type,aggregate_id,event_type,payload,created_at,attempts FROM "+s.table(table)+" WHERE status='pending' AND next_attempt_at<=CURRENT_TIMESTAMP(6) AND(lease_until IS NULL OR lease_until<=CURRENT_TIMESTAMP(6)) ORDER BY next_attempt_at,created_at,id LIMIT 1 FOR UPDATE SKIP LOCKED").Scan(uid(&v.ID), uid(&v.TenantID), &v.AggregateType, uid(&aggregate), &v.EventType, &v.Payload, &v.CreatedAt, &v.Attempt)
			if errors.Is(e, sql.ErrNoRows) {
				continue
			}
			if e != nil {
				return false, e
			}
			v.AggregateID = aggregate.String()
			v.Attempt++
			_, e = s.db.ExecContext(ctx, "UPDATE "+s.table(table)+" SET attempts=?,lease_token=?,lease_until=DATE_ADD(CURRENT_TIMESTAMP(6),INTERVAL 60 SECOND) WHERE id=?", v.Attempt, id(v.Token), id(v.ID))
			if e != nil {
				return false, e
			}
			out = &v
			return true, nil
		}
		return false, nil
	})
	return out, e
}
func (m Maintenance) FinishDelivery(ctx context.Context, v Delivery, failure string) error {
	repos, e := NewRepositories(m.DB, m.Database)
	if e != nil {
		return e
	}
	s := repos.Cases.(caseRepo).scope
	table := "outbox_events"
	if v.inbox {
		table = "inbox_outbox"
	}
	status := "sent"
	if failure != "" {
		status = "pending"
		if v.Attempt >= 12 {
			status = "failed"
		}
	}
	delay := 1 << min(v.Attempt, 12)
	result, e := s.db.ExecContext(ctx, "UPDATE "+s.table(table)+" SET status=?,next_attempt_at=DATE_ADD(CURRENT_TIMESTAMP(6),INTERVAL ? SECOND),lease_token=NULL,lease_until=NULL,last_error=NULLIF(?,''),sent_at=IF(?='sent',CURRENT_TIMESTAMP(6),NULL) WHERE tenant_id=? AND id=? AND lease_token=? AND status='pending' AND lease_until>CURRENT_TIMESTAMP(6)", status, delay, failure, status, id(v.TenantID), id(v.ID), id(v.Token))
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e == nil && n != 1 {
		return c.ErrConflict
	}
	return e
}
func (m Maintenance) Replay(ctx context.Context, t, i uuid.UUID, inbox bool) error {
	repos, e := NewRepositories(m.DB, m.Database)
	if e != nil {
		return e
	}
	s := repos.Cases.(caseRepo).scope
	table := "outbox_events"
	if inbox {
		table = "inbox_outbox"
	}
	result, e := s.db.ExecContext(ctx, "UPDATE "+s.table(table)+" SET status='pending',attempts=0,next_attempt_at=CURRENT_TIMESTAMP(6),last_error=NULL,sent_at=NULL WHERE tenant_id=? AND id=? AND status IN('failed','sent') AND lease_token IS NULL", id(t), id(i))
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e == nil && n != 1 {
		return c.ErrConflict
	}
	return e
}
