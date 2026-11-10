package mysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	d "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
)

func scopeHash(v d.ExclusionScope) []byte {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return h[:]
}
func (r exclusions) Active(ctx context.Context, v d.ExclusionScope, entities []string) (map[string]string, error) {
	out := map[string]string{}
	if len(entities) == 0 {
		return out, nil
	}
	if len(entities) > 500 {
		return nil, ErrLimit
	}
	a := []any{id(v.TenantID), scopeHash(v), v.ObjectType, []byte(v.ObjectID), v.Provider, []byte(v.QueryFingerprint)}
	for _, entity := range entities {
		a = append(a, []byte(entity))
	}
	rows, e := r.q.QueryContext(ctx, "SELECT entity_id,operation_id FROM "+r.table("object_exclusions")+" WHERE tenant_id=? AND scope_hash=? AND object_type=? AND object_id=? AND provider=? AND query_fingerprint=? AND active AND entity_id IN("+placeholders(len(entities))+")", a...)
	if e != nil {
		return nil, normalize(e)
	}
	defer rows.Close()
	for rows.Next() {
		var entity, op string
		if e = rows.Scan(&entity, uid(&op)); e != nil {
			return nil, normalize(e)
		}
		out[entity] = op
	}
	return out, normalize(rows.Err())
}
func (r exclusions) Set(ctx context.Context, v d.ExclusionChange) error {
	h := scopeHash(v.Scope)
	var old d.ExclusionScope
	var active bool
	e := r.q.QueryRowContext(ctx, "SELECT object_type,object_id,provider,query_fingerprint,active FROM "+r.table("object_exclusions")+" WHERE tenant_id=? AND scope_hash=? AND entity_id=? FOR UPDATE", id(v.Scope.TenantID), h, []byte(v.EntityID)).Scan(&old.ObjectType, &old.ObjectID, &old.Provider, &old.QueryFingerprint, &active)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return normalize(e)
	}
	if e == nil && (old.ObjectType != v.Scope.ObjectType || old.ObjectID != v.Scope.ObjectID || old.Provider != v.Scope.Provider || old.QueryFingerprint != v.Scope.QueryFingerprint) {
		return ports.ErrExclusionConflict
	}
	if e == nil && active == v.Active || errors.Is(e, sql.ErrNoRows) && !v.Active {
		return ports.ErrExclusionConflict
	}
	_, e = r.q.ExecContext(ctx, "INSERT INTO "+r.table("object_exclusion_events")+"(tenant_id,operation_id,screening_id,match_id,actor_id,scope_hash,object_type,object_id,provider,query_fingerprint,entity_id,active,findings,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,UTC_TIMESTAMP(6))", id(v.Scope.TenantID), id(v.OperationID), id(v.ScreeningID), id(v.MatchID), id(v.ActorID), h, v.Scope.ObjectType, []byte(v.Scope.ObjectID), v.Scope.Provider, []byte(v.Scope.QueryFingerprint), []byte(v.EntityID), v.Active, v.Findings)
	if e != nil {
		return normalize(e)
	}
	_, e = r.q.ExecContext(ctx, "INSERT INTO "+r.table("object_exclusions")+"(tenant_id,scope_hash,entity_id,object_type,object_id,provider,query_fingerprint,active,operation_id,updated_at) VALUES(?,?,?,?,?,?,?,?,?,UTC_TIMESTAMP(6)) ON DUPLICATE KEY UPDATE active=VALUES(active),operation_id=VALUES(operation_id),updated_at=VALUES(updated_at)", id(v.Scope.TenantID), h, []byte(v.EntityID), v.Scope.ObjectType, []byte(v.Scope.ObjectID), v.Scope.Provider, []byte(v.Scope.QueryFingerprint), v.Active, id(v.OperationID))
	return normalize(e)
}
func (r exclusions) RecordApplications(ctx context.Context, t, s string, applied map[string]string) error {
	if len(applied) > 500 {
		return ErrLimit
	}
	for match, operation := range applied {
		_, e := r.q.ExecContext(ctx, "INSERT INTO "+r.table("object_exclusion_applications")+"(tenant_id,screening_id,match_id,operation_id,applied_at) VALUES(?,?,?,?,UTC_TIMESTAMP(6))", id(t), id(s), id(matchKey(match)), id(operation))
		if e != nil {
			return normalize(e)
		}
	}
	return nil
}
