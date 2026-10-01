package postgres

import (
	"context"
	"encoding/json"
	"time"

	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
)

type MembershipRepository struct{ db queryable }

func (r MembershipRepository) RecordChange(ctx context.Context, tenantID, inboxID uuid.UUID, actor *string, kind, userID string, autoAssign bool, at time.Time, capacity int) error {
	if actor == nil {
		return casepkg.ErrForbidden
	}
	payload, err := json.Marshal(map[string]any{"user_id": userID, "auto_assign_enabled": autoAssign, "capacity": capacity})
	if err != nil {
		return err
	}
	id := uuid.New()
	if _, err = r.db.Exec(ctx, `INSERT INTO case_manager.inbox_events(id,tenant_id,inbox_id,actor_id,event_type,payload,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, tenantID, inboxID, *actor, kind, payload, at); err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `INSERT INTO case_manager.outbox_events(id,tenant_id,aggregate_type,aggregate_id,event_type,payload,status,created_at) VALUES($1,$2,'inbox',$3,$4,$5,'pending',$6)`, id, tenantID, inboxID.String(), kind, payload, at)
	return err
}
func (r MembershipRepository) Has(ctx context.Context, tenantID, inboxID uuid.UUID, userID string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM case_manager.inbox_users WHERE tenant_id=$1 AND inbox_id=$2 AND user_id=$3)`, tenantID, inboxID, userID).Scan(&exists)
	return exists, err
}
func (r MembershipRepository) List(ctx context.Context, tenantID, inboxID uuid.UUID) ([]casepkg.InboxUser, error) {
	rows, err := r.db.Query(ctx, `SELECT id,tenant_id,inbox_id,user_id,auto_assign_enabled,created_at,capacity FROM case_manager.inbox_users WHERE tenant_id=$1 AND inbox_id=$2 ORDER BY user_id`, tenantID, inboxID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []casepkg.InboxUser{}
	for rows.Next() {
		var m casepkg.InboxUser
		if err := rows.Scan(&m.ID, &m.TenantID, &m.InboxID, &m.UserID, &m.AutoAssignEnabled, &m.CreatedAt, &m.Capacity); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func (r MembershipRepository) Put(ctx context.Context, m casepkg.InboxUser) (casepkg.InboxUser, error) {
	err := r.db.QueryRow(ctx, `INSERT INTO case_manager.inbox_users(id,tenant_id,inbox_id,user_id,auto_assign_enabled,created_at,capacity) VALUES($1,$2,$3,$4,$5,$6,$7)
	ON CONFLICT(tenant_id,inbox_id,user_id) DO UPDATE SET auto_assign_enabled=EXCLUDED.auto_assign_enabled,capacity=EXCLUDED.capacity
	RETURNING id,created_at`, m.ID, m.TenantID, m.InboxID, m.UserID, m.AutoAssignEnabled, m.CreatedAt, m.Capacity).Scan(&m.ID, &m.CreatedAt)
	return m, err
}
func (r MembershipRepository) Remove(ctx context.Context, tenantID, inboxID uuid.UUID, userID string) error {
	var assigned bool
	if err := r.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM case_manager.cases WHERE tenant_id=$1 AND inbox_id=$2 AND assigned_to=$3 AND status<>'closed')`, tenantID, inboxID, userID).Scan(&assigned); err != nil {
		return err
	}
	if assigned {
		return casepkg.ErrConflict
	}
	tag, err := r.db.Exec(ctx, `DELETE FROM case_manager.inbox_users WHERE tenant_id=$1 AND inbox_id=$2 AND user_id=$3`, tenantID, inboxID, userID)
	if err == nil && tag.RowsAffected() == 0 {
		return casepkg.ErrNotFound
	}
	return err
}

type ContributorRepository struct{ db queryable }

func (r ContributorRepository) Add(ctx context.Context, tenantID, caseID uuid.UUID, userID string, at time.Time) error {
	_, err := r.db.Exec(ctx, `INSERT INTO case_manager.case_contributors(id,tenant_id,case_id,user_id,created_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,case_id,user_id) DO NOTHING`, uuid.New(), tenantID, caseID, userID, at)
	return err
}
func (r ContributorRepository) List(ctx context.Context, tenantID, caseID uuid.UUID) ([]casepkg.Contributor, error) {
	rows, err := r.db.Query(ctx, `SELECT user_id,created_at FROM case_manager.case_contributors WHERE tenant_id=$1 AND case_id=$2 ORDER BY created_at,user_id`, tenantID, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []casepkg.Contributor{}
	for rows.Next() {
		var c casepkg.Contributor
		if err := rows.Scan(&c.UserID, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r TagRepository) ListByCases(ctx context.Context, tenantID uuid.UUID, caseIDs []uuid.UUID) (map[uuid.UUID][]casepkg.Tag, error) {
	out := make(map[uuid.UUID][]casepkg.Tag, len(caseIDs))
	if len(caseIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `SELECT ct.case_id,t.id,t.tenant_id,t.target,t.name,t.color,t.deleted_at,t.created_at,t.updated_at
	FROM case_manager.case_tags ct JOIN case_manager.tags t ON t.id=ct.tag_id AND t.tenant_id=ct.tenant_id
	WHERE ct.tenant_id=$1 AND ct.case_id=ANY($2) AND ct.deleted_at IS NULL ORDER BY ct.case_id,t.name,t.id`, tenantID, caseIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var tag casepkg.Tag
		if err := rows.Scan(&id, &tag.ID, &tag.TenantID, &tag.Target, &tag.Name, &tag.Color, &tag.DeletedAt, &tag.CreatedAt, &tag.UpdatedAt); err != nil {
			return nil, err
		}
		out[id] = append(out[id], tag)
	}
	return out, rows.Err()
}
