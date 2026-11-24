package postgres

import (
	"context"
	"encoding/json"
	"errors"
	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type ClientCaseSummary struct {
	ID        uuid.UUID `json:"id"`
	InboxID   uuid.UUID `json:"inbox_id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Outcome   string    `json:"outcome"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
type ClientCasePage struct {
	Cases     []ClientCaseSummary `json:"cases"`
	NextAfter string              `json:"next_after"`
}
type ClientAccessRepository struct{ DB *pgxpool.Pool }

// At most 256 granted inboxes, each with an indexed page of 101 summaries.
// No history, evidence, assignment data, or arbitrary relationship scan.
const ClientCaseListSQL = `SELECT c.id,c.inbox_id,c.name,c.status,c.outcome,c.created_at,c.updated_at
 FROM case_manager.client_inbox_access a CROSS JOIN LATERAL (
 SELECT id,inbox_id,name,status,outcome,created_at,updated_at FROM case_manager.cases
 WHERE tenant_id=a.tenant_id AND inbox_id=a.inbox_id AND id>$3 ORDER BY id LIMIT 101
 ) c WHERE a.tenant_id=$1 AND a.client_id=$2 ORDER BY c.id LIMIT 101`

func scanClientCase(row pgx.Row) (ClientCaseSummary, error) {
	var c ClientCaseSummary
	err := row.Scan(&c.ID, &c.InboxID, &c.Name, &c.Status, &c.Outcome, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}
func (r ClientAccessRepository) GetCase(ctx context.Context, tenant, client, id uuid.UUID) (ClientCaseSummary, error) {
	item, err := scanClientCase(r.DB.QueryRow(ctx, `SELECT c.id,c.inbox_id,c.name,c.status,c.outcome,c.created_at,c.updated_at
 FROM case_manager.cases c JOIN case_manager.client_inbox_access a ON a.tenant_id=c.tenant_id AND a.inbox_id=c.inbox_id
 WHERE c.tenant_id=$1 AND c.id=$2 AND a.client_id=$3`, tenant, id, client))
	if errors.Is(err, pgx.ErrNoRows) {
		return item, auth.Forbidden()
	}
	return item, err
}
func (r ClientAccessRepository) ListCases(ctx context.Context, tenant, client, after uuid.UUID) (ClientCasePage, error) {
	out := ClientCasePage{Cases: []ClientCaseSummary{}}
	rows, err := r.DB.Query(ctx, ClientCaseListSQL, tenant, client, after)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		c, err := scanClientCase(rows)
		if err != nil {
			return out, err
		}
		out.Cases = append(out.Cases, c)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	if len(out.Cases) > 100 {
		out.NextAfter = out.Cases[99].ID.String()
		out.Cases = out.Cases[:100]
	}
	return out, nil
}
func (r ClientAccessRepository) ListInboxes(ctx context.Context, p auth.Principal, client uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.DB.Query(ctx, `SELECT a.inbox_id FROM case_manager.client_inbox_access a
 WHERE a.tenant_id=$1 AND a.client_id=$2 AND ($3 OR EXISTS(SELECT 1 FROM case_manager.inbox_users u
 WHERE u.tenant_id=a.tenant_id AND u.inbox_id=a.inbox_id AND u.user_id=$4)) ORDER BY a.inbox_id LIMIT 257`, p.TenantID, client, p.Can("cases:access-all"), p.Subject)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	if len(out) > 256 {
		return nil, errors.New("client inbox limit violated")
	}
	return out, rows.Err()
}

// SetInbox owns the grant limit, resource authorization and atomic audit. Only
// this client's grants serialize; unrelated clients and inboxes remain independent.
func (r ClientAccessRepository) SetInbox(ctx context.Context, p auth.Principal, client, inbox uuid.UUID, grant bool) error {
	if !p.IsHuman() || !p.Can("inboxes:manage") {
		return auth.Forbidden()
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if err = auth.CheckCapability(ctx, tx, p, "inboxes:manage", true); err != nil {
		return err
	}
	if p.Can("cases:access-all") {
		if err = auth.CheckCapability(ctx, tx, p, "cases:access-all", true); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "case-client-access:"+p.TenantID.String()+":"+client.String()); err != nil {
		return err
	}
	var accessible bool
	err = tx.QueryRow(ctx, `SELECT ($3 OR EXISTS(SELECT 1 FROM case_manager.inbox_users u WHERE u.tenant_id=i.tenant_id AND u.inbox_id=i.id AND u.user_id=$4)) FROM case_manager.inboxes i WHERE i.tenant_id=$1 AND i.id=$2 FOR SHARE`, p.TenantID, inbox, p.Can("cases:access-all"), p.Subject).Scan(&accessible)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !accessible {
		return auth.Forbidden()
	}
	if err != nil {
		return err
	}
	kind := "client_access_revoked"
	var affected int64
	if grant {
		var count int
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM case_manager.client_inbox_access WHERE tenant_id=$1 AND client_id=$2`, p.TenantID, client).Scan(&count); err != nil {
			return err
		}
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM case_manager.client_inbox_access WHERE tenant_id=$1 AND client_id=$2 AND inbox_id=$3)`, p.TenantID, client, inbox).Scan(&exists); err != nil {
			return err
		}
		if count >= 256 && !exists {
			return &auth.Error{Code: 409, Message: "Client inbox limit reached."}
		}
		tag, e := tx.Exec(ctx, `INSERT INTO case_manager.client_inbox_access(tenant_id,client_id,inbox_id,granted_by) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, p.TenantID, client, inbox, p.Subject)
		err = e
		affected = tag.RowsAffected()
		kind = "client_access_granted"
	} else {
		tag, e := tx.Exec(ctx, `DELETE FROM case_manager.client_inbox_access WHERE tenant_id=$1 AND client_id=$2 AND inbox_id=$3`, p.TenantID, client, inbox)
		err = e
		affected = tag.RowsAffected()
	}
	if err != nil {
		return err
	}
	if affected > 0 {
		payload, err := json.Marshal(map[string]any{"client_id": client})
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO case_manager.inbox_events(id,tenant_id,inbox_id,actor_id,event_type,payload,created_at) VALUES($1,$2,$3,$4,$5,$6,now())`, uuid.New(), p.TenantID, inbox, p.Subject, kind, payload); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
