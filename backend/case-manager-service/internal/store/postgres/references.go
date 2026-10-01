package postgres

import (
	"context"
	"errors"

	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ReferenceRepository struct{ db queryable }

func (r ReferenceRepository) Decisions(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]casepkg.DecisionReference, error) {
	out := make(map[uuid.UUID]casepkg.DecisionReference, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `SELECT id,scenario_id,object_type,object_id FROM core.decisions WHERE tenant_id=$1 AND id=ANY($2) ORDER BY id FOR SHARE`, tenantID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d casepkg.DecisionReference
		if err := rows.Scan(&d.ID, &d.ScenarioID, &d.ObjectType, &d.ObjectID); err != nil {
			return nil, err
		}
		out[d.ID] = d
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		if _, ok := out[id]; !ok {
			return nil, casepkg.ErrNotFound
		}
	}
	return out, nil
}

func (r ReferenceRepository) Screening(ctx context.Context, tenantID, screeningID uuid.UUID, matchID string) (casepkg.ScreeningReference, error) {
	var ref casepkg.ScreeningReference
	var objectType, objectID string
	err := r.db.QueryRow(ctx, `SELECT s.id,s.decision_id,s.object_type,s.object_id,m.status FROM screening.screenings s
	JOIN screening.screening_matches m ON m.screening_id=s.id AND m.tenant_id=s.tenant_id
	WHERE s.tenant_id=$1 AND s.id=$2 AND m.id=$3 FOR SHARE OF s,m`, tenantID, screeningID, matchID).Scan(&ref.ID, &ref.DecisionID, &objectType, &objectID, &ref.MatchStatus)
	if err != nil {
		return ref, err
	}
	// An unambiguous configured route is required when there is no existing case.
	rows, err := r.db.Query(ctx, `SELECT DISTINCT c.review_inbox_id FROM screening.continuous_screening_configs c
	JOIN screening.monitored_objects m ON m.config_id=c.id AND m.tenant_id=c.tenant_id
	WHERE c.tenant_id=$1 AND m.object_type=$2 AND m.object_id=$3 AND c.enabled AND c.review_inbox_id IS NOT NULL LIMIT 2`, tenantID, objectType, objectID)
	if err != nil {
		return ref, err
	}
	defer rows.Close()
	var routes []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return ref, err
		}
		routes = append(routes, value)
	}
	if err := rows.Err(); err != nil {
		return ref, err
	}
	if len(routes) == 1 {
		id, err := uuid.Parse(routes[0])
		if err != nil || id == uuid.Nil {
			return ref, casepkg.Invalid("invalid configured review inbox")
		}
		ref.ReviewInboxID = &id
	}
	return ref, nil
}

func (r ReferenceRepository) ScreeningFile(ctx context.Context, tenantID, caseID, fileID uuid.UUID) (casepkg.File, error) {
	f := casepkg.File{TenantID: tenantID, CaseID: caseID, SourceFileID: &fileID}
	err := r.db.QueryRow(ctx, `SELECT f.file_name,f.content_type,f.file_size,f.storage_key,f.uploaded_by FROM screening.screening_files f
	JOIN screening.screenings s ON s.id=f.screening_id AND s.tenant_id=f.tenant_id
	WHERE f.tenant_id=$1 AND f.id=$3 AND (
	EXISTS(SELECT 1 FROM case_manager.case_screenings cs WHERE cs.tenant_id=$1 AND cs.case_id=$2 AND cs.screening_id=f.screening_id)
	OR EXISTS(SELECT 1 FROM case_manager.case_decisions cd WHERE cd.tenant_id=$1 AND cd.case_id=$2 AND cd.decision_id=s.decision_id))
	FOR SHARE OF f,s`, tenantID, caseID, fileID).Scan(&f.FileName, &f.ContentType, &f.FileSize, &f.StorageKey, &f.UploadedBy)
	return f, err
}

func (r DecisionLinkRepository) FindCase(ctx context.Context, tenantID, decisionID uuid.UUID) (*casepkg.Case, error) {
	return findLinkedCase(ctx, r.db, `SELECT c.id FROM case_manager.case_decisions d JOIN case_manager.cases c ON c.id=d.case_id AND c.tenant_id=d.tenant_id
	WHERE d.tenant_id=$1 AND d.decision_id=$2 ORDER BY (c.status='closed'),c.created_at DESC,c.id DESC LIMIT 1`, tenantID, decisionID)
}
func (r ScreeningLinkRepository) FindCase(ctx context.Context, tenantID, screeningID uuid.UUID) (*casepkg.Case, error) {
	return findLinkedCase(ctx, r.db, `SELECT c.id FROM case_manager.case_screenings s JOIN case_manager.cases c ON c.id=s.case_id AND c.tenant_id=s.tenant_id
	WHERE s.tenant_id=$1 AND s.screening_id=$2 ORDER BY (c.status='closed'),c.created_at DESC,c.id DESC LIMIT 1`, tenantID, screeningID)
}
func findLinkedCase(ctx context.Context, db queryable, query string, tenantID, resourceID uuid.UUID) (*casepkg.Case, error) {
	var id uuid.UUID
	if err := db.QueryRow(ctx, query, tenantID, resourceID).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	c, err := NewCaseRepository(db).Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	return &c, nil
}
