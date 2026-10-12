package postgres

import (
	"context"
	"errors"
	"time"

	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/ports"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ContinuousScreening reads only owner-published provenance. Current review
// status is deliberately not eligibility: delivery may follow human review.
func (r ReferenceRepository) ContinuousScreening(ctx context.Context, tenant, id uuid.UUID) (*ports.ContinuousScreeningReference, error) {
	eventID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("screening.requires-review/v1/"+tenant.String()+"/"+id.String()))
	var ref ports.ContinuousScreeningReference
	err := r.db.QueryRow(ctx, `SELECT c.review_inbox_id,c.enabled,
 EXISTS(SELECT 1 FROM screening.case_event_outbox e WHERE e.id=$3 AND e.tenant_id=$1 AND e.kind='requires-review' AND e.payload->>'screening_id'=$2::uuid::text)
 FROM screening.continuous_runs r JOIN screening.continuous_screening_configs c ON c.tenant_id=r.tenant_id AND c.id=r.config_id
 WHERE r.tenant_id=$1 AND r.screening_id=$2 FOR SHARE OF c`, tenant, id, eventID).Scan(&ref.ReviewInboxID, &ref.Enabled, &ref.Published)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &ref, err
}

func (r ScreeningLinkRepository) CanonicalCase(ctx context.Context, tenant, id uuid.UUID) (*casepkg.Case, error) {
	// Detect historical ambiguity; never silently pick the newest case.
	rows, err := r.db.Query(ctx, `SELECT case_id FROM case_manager.screening_intake_routes WHERE tenant_id=$1 AND screening_id=$2
 UNION SELECT case_id FROM case_manager.case_screenings WHERE tenant_id=$1 AND screening_id=$2 LIMIT 2`, tenant, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var v uuid.UUID
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		ids = append(ids, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) > 1 {
		return nil, casepkg.ErrConflict
	}
	if len(ids) == 0 {
		return nil, nil
	}
	item, err := NewCaseRepository(r.db).Get(ctx, tenant, ids[0])
	return &item, err
}
func (r ScreeningLinkRepository) BindContinuous(ctx context.Context, tenant, id, caseID uuid.UUID) (bool, error) {
	tag, err := r.db.Exec(ctx, `INSERT INTO case_manager.screening_intake_routes(tenant_id,screening_id,case_id) VALUES($1,$2,$3)
 ON CONFLICT(tenant_id,screening_id) DO NOTHING`, tenant, id, caseID)
	return tag.RowsAffected() == 1, err
}
func (r ScreeningLinkRepository) SyncContinuous(ctx context.Context, tenant, id, caseID uuid.UUID, at time.Time) error {
	// The caller holds the screening intake lock. Lock source rows before reading
	// statuses so a concurrent owner review is observed here or by its callback.
	rows, err := r.db.Query(ctx, `SELECT id FROM screening.screening_matches WHERE tenant_id=$1 AND screening_id=$2 ORDER BY id FOR SHARE`, tenant, id)
	if err != nil {
		return err
	}
	for rows.Next() {
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `INSERT INTO case_manager.case_screenings(id,tenant_id,case_id,screening_id,match_id,status,partial,created_at)
 SELECT gen_random_uuid(),s.tenant_id,$3,s.id,m.id,m.status,s.partial,$4
 FROM screening.screenings s JOIN screening.screening_matches m ON m.tenant_id=s.tenant_id AND m.screening_id=s.id
 WHERE s.tenant_id=$1 AND s.id=$2
 ON CONFLICT(tenant_id,case_id,screening_id,match_id) WHERE match_id IS NOT NULL DO UPDATE SET status=EXCLUDED.status,partial=EXCLUDED.partial`, tenant, id, caseID, at)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `INSERT INTO case_manager.case_screenings(id,tenant_id,case_id,screening_id,match_id,status,partial,created_at)
 SELECT gen_random_uuid(),tenant_id,$3,id,NULL,status,partial,$4 FROM screening.screenings s
 WHERE tenant_id=$1 AND id=$2 AND NOT EXISTS(SELECT 1 FROM screening.screening_matches m WHERE m.tenant_id=$1 AND m.screening_id=$2)
 ON CONFLICT(tenant_id,case_id,screening_id) WHERE match_id IS NULL DO UPDATE SET status=EXCLUDED.status,partial=EXCLUDED.partial`, tenant, id, caseID, at)
	return err
}
