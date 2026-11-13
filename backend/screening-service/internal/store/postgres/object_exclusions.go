package postgres

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
)

type ObjectExclusionRepository struct{ q queryable }

func (r ObjectExclusionRepository) Active(ctx context.Context, s screening.ExclusionScope, entities []string) (map[string]string, error) {
	rows, err := r.q.Query(ctx, `SELECT entity_id,operation_id::text FROM screening.object_exclusions
WHERE tenant_id=$1 AND object_type=$2 AND object_id=$3 AND provider=$4 AND query_fingerprint=$5 AND active AND entity_id=ANY($6::text[])`,
		s.TenantID, s.ObjectType, s.ObjectID, s.Provider, s.QueryFingerprint, entities)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, operation string
		if err := rows.Scan(&id, &operation); err != nil {
			return nil, err
		}
		out[id] = operation
	}
	return out, rows.Err()
}

func (r ObjectExclusionRepository) Set(ctx context.Context, change screening.ExclusionChange) error {
	s := change.Scope
	tag, err := r.q.Exec(ctx, `INSERT INTO screening.object_exclusions
(tenant_id,object_type,object_id,provider,query_fingerprint,entity_id,active,operation_id)
VALUES($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT(tenant_id,object_type,object_id,provider,query_fingerprint,entity_id)
DO UPDATE SET active=EXCLUDED.active,operation_id=EXCLUDED.operation_id,updated_at=now()
WHERE screening.object_exclusions.active<>EXCLUDED.active`, s.TenantID, s.ObjectType, s.ObjectID, s.Provider, s.QueryFingerprint, change.EntityID, change.Active, change.OperationID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ports.ErrExclusionConflict
	}
	_, err = r.q.Exec(ctx, `INSERT INTO screening.object_exclusion_events
(tenant_id,operation_id,screening_id,match_id,actor_id,object_type,object_id,provider,query_fingerprint,entity_id,active,findings)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		s.TenantID, change.OperationID, change.ScreeningID, change.MatchID, change.ActorID, s.ObjectType, s.ObjectID, s.Provider, s.QueryFingerprint, change.EntityID, change.Active, change.Findings)
	return err
}

func (r ObjectExclusionRepository) RecordApplications(ctx context.Context, tenant, screeningID string, applied map[string]string) error {
	if len(applied) == 0 {
		return nil
	}
	matches, operations := []string{}, []string{}
	for match, operation := range applied {
		matches = append(matches, match)
		operations = append(operations, operation)
	}
	_, err := r.q.Exec(ctx, `INSERT INTO screening.object_exclusion_applications(tenant_id,screening_id,match_id,operation_id)
SELECT $1,$2,x.match_id,x.operation_id::uuid FROM unnest($3::text[],$4::text[]) AS x(match_id,operation_id)`, tenant, screeningID, matches, operations)
	return err
}
