package postgres

import (
	"context"
	"errors"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/ports"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Validate owner identity, including scenario/config and screened object. A
// caller cannot supply routing or decision identity in an arrival payload.
func (r ReferenceRepository) DecisionScreening(ctx context.Context, tenant, id uuid.UUID) (*ports.DecisionScreeningReference, error) {
	var ref ports.DecisionScreeningReference
	var valid bool
	event := uuid.NewSHA1(uuid.NameSpaceURL, []byte("screening.requires-review/v1/"+tenant.String()+"/"+id.String()))
	err := r.db.QueryRow(ctx, `SELECT s.decision_id,c.review_inbox_id,
 EXISTS(SELECT 1 FROM screening.case_event_outbox e WHERE e.id=$3 AND e.tenant_id=$1 AND e.kind='requires-review' AND e.payload->>'screening_id'=$2::uuid::text),
 COALESCE(d.id IS NOT NULL AND c.id IS NOT NULL AND d.scenario_id=s.scenario_id AND c.scenario_id=d.scenario_id AND d.object_type=s.object_type AND d.object_id=s.object_id,false)
 FROM screening.screenings s
 LEFT JOIN core.decisions d ON d.tenant_id=s.tenant_id AND d.id=s.decision_id
 LEFT JOIN core.screening_configs c ON c.tenant_id=s.tenant_id AND c.id=s.screening_config_id
 WHERE s.tenant_id=$1 AND s.id=$2 AND s.decision_id IS NOT NULL`, tenant, id, event).Scan(&ref.DecisionID, &ref.ReviewInboxID, &ref.Published, &valid)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// Legacy reviewed events have no arrival provenance and retain their existing
	// primary-case-only synchronization path. New published arrivals are strict.
	if ref.Published && !valid {
		return nil, casepkg.Invalid("screening decision/configuration provenance does not match")
	}
	return &ref, nil
}
