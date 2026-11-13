package postgres

import (
	"context"
	"errors"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Read-only owner contract. The optional row locks are taken after the new case
// lock, matching investigator case -> screening -> candidate ordering.
func (r ReferenceRepository) ManualScreening(ctx context.Context, tenant, screening, candidate uuid.UUID, actor string, lock bool) (bool, error) {
	if lock {
		var id uuid.UUID
		err := r.db.QueryRow(ctx, `SELECT id FROM screening.screenings WHERE tenant_id=$1 AND id=$2 FOR SHARE`, tenant, screening).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, casepkg.ErrNotFound
		}
		if err != nil {
			return false, err
		}
	}
	query := `SELECT s.status='awaiting_review' AND NOT s.is_archived AND m.status='pending'
 FROM screening.screenings s JOIN screening.screening_matches m ON m.tenant_id=s.tenant_id AND m.screening_id=s.id
 WHERE s.tenant_id=$1 AND s.id=$2 AND m.id=$3 AND s.is_manual
 AND s.decision_id IS NULL AND s.scenario_id IS NULL
 AND s.idempotency_key LIKE 'manual:%' AND split_part(s.idempotency_key,':',2)=$4`
	if lock {
		query += ` FOR SHARE OF m`
	}
	var eligible bool
	err := r.db.QueryRow(ctx, query, tenant, screening, candidate, actor).Scan(&eligible)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, casepkg.ErrNotFound
	}
	return eligible, err
}
