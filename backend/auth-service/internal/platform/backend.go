package platform

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/internal/oidcprovider"
	"github.com/google/uuid"
)

// HTTPStore keeps storage and transaction ownership behind the existing wire contract.
type HTTPStore interface {
	Ready(context.Context) error
	BeginLogin(context.Context, string) (Login, error)
	ConsumeLogin(context.Context, string, string, string) (Login, error)
	CreateSessionForTenant(context.Context, string, string, uuid.UUID) (string, error)
	Resolve(context.Context, string) (Session, error)
	Logout(context.Context, string) error
	Sessions(context.Context, string, uuid.UUID) ([]SessionInfo, error)
	RevokeSessions(context.Context, string, uuid.UUID, bool) (bool, error)
	Members(context.Context, string, uuid.UUID) ([]Member, error)
	ChangeMember(context.Context, string, uuid.UUID, int64, []string, bool) error
	Audit(context.Context, string, uuid.UUID) ([]AuditEvent, error)
}

type InvitationBackend interface {
	Begin(context.Context, string) (Login, error)
	Accept(context.Context, []byte, oidcprovider.Identity) (string, error)
	Create(context.Context, string, uuid.UUID, string, []string) (uuid.UUID, error)
	Change(context.Context, string, uuid.UUID, int64, string) error
	List(context.Context, string, uuid.UUID) ([]Invitation, error)
}

type AuditEvent struct {
	ID      uuid.UUID       `json:"id"`
	Actor   string          `json:"actor"`
	Action  string          `json:"action"`
	Target  uuid.UUID       `json:"target"`
	At      string          `json:"at"`
	Details json.RawMessage `json:"details"`
}

func (s Store) Ready(ctx context.Context) error {
	var version int
	if err := s.DB.QueryRow(ctx, "SELECT max(version) FROM identity.schema_migrations").Scan(&version); err != nil {
		return err
	}
	if version != SchemaVersion {
		return errors.New("identity schema version mismatch")
	}
	return nil
}

func (s Store) Audit(ctx context.Context, raw string, after uuid.UUID) ([]AuditEvent, error) {
	c, err := s.Resolve(ctx, raw)
	if err != nil {
		return nil, err
	}
	if !hasRole(c.Roles, "tenant_admin") {
		return nil, ErrDenied
	}
	rows, err := s.DB.Query(ctx, `SELECT id,actor_id,action,target_id,created_at::text,details FROM identity.audit_events WHERE tenant_id=$1 AND id>$2 ORDER BY id LIMIT 101`, c.TenantID, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEvent{}
	for rows.Next() {
		var e AuditEvent
		if err := rows.Scan(&e.ID, &e.Actor, &e.Action, &e.Target, &e.At, &e.Details); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func hasRole(roles []string, role string) bool {
	for _, r := range roles {
		if r == role {
			return true
		}
	}
	return false
}
