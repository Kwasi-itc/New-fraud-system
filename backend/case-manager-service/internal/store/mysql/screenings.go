package mysql

import (
	"context"
	"database/sql"
	"errors"
	"time"

	c "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

type screeningRepo struct{ scope }

const screeningCols = "id,tenant_id,case_id,screening_id,match_id,status,created_at"

func scanScreening(r row) (c.ScreeningLink, error) {
	var v c.ScreeningLink
	var match mysqlstore.NullUUID
	e := r.Scan(uid(&v.ID), uid(&v.TenantID), uid(&v.CaseID), uid(&v.ScreeningID), &match, &v.Status, &v.CreatedAt)
	if match.Valid {
		s := uuid.UUID(match.UUID).String()
		v.MatchID = &s
	}
	return v, normalized(e)
}
func (r screeningRepo) Create(ctx context.Context, v c.ScreeningLink) (c.ScreeningLink, error) {
	var match any
	if v.MatchID != nil {
		u, e := uuid.Parse(*v.MatchID)
		if e != nil || u == uuid.Nil {
			return c.ScreeningLink{}, c.Invalid("invalid screening match ID")
		}
		match = id(u)
	}
	_, e := r.db.ExecContext(ctx, "INSERT INTO "+r.table("case_screenings")+"("+screeningCols+") VALUES(?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE status=?", id(v.ID), id(v.TenantID), id(v.CaseID), id(v.ScreeningID), match, v.Status, v.CreatedAt, v.Status)
	if e != nil {
		return c.ScreeningLink{}, normalized(e)
	}
	return scanScreening(r.db.QueryRowContext(ctx, "SELECT "+screeningCols+" FROM "+r.table("case_screenings")+" WHERE tenant_id=? AND case_id=? AND screening_id=? AND match_id<=>?", id(v.TenantID), id(v.CaseID), id(v.ScreeningID), match))
}
func (r screeningRepo) ListByCase(ctx context.Context, t, i uuid.UUID) ([]c.ScreeningLink, error) {
	rows, e := r.db.QueryContext(ctx, "SELECT "+screeningCols+" FROM "+r.table("case_screenings")+" WHERE tenant_id=? AND case_id=? ORDER BY created_at DESC,id DESC LIMIT 501", id(t), id(i))
	return collectLimited(rows, e, scanScreening, 500)
}
func (r screeningRepo) CanonicalCase(ctx context.Context, t, i uuid.UUID) (*c.Case, error) {
	rows, e := r.db.QueryContext(ctx, "SELECT case_id FROM "+r.table("continuous_bindings")+" WHERE tenant_id=? AND screening_id=? UNION SELECT case_id FROM "+r.table("case_screenings")+" WHERE tenant_id=? AND screening_id=? LIMIT 2", id(t), id(i), id(t), id(i))
	if e != nil {
		return nil, e
	}
	var ids []uuid.UUID
	for rows.Next() {
		var v uuid.UUID
		if e = rows.Scan(uid(&v)); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	if len(ids) > 1 {
		return nil, c.ErrConflict
	}
	if len(ids) == 0 {
		return nil, nil
	}
	v, e := (caseRepo{r.scope}).Get(ctx, t, ids[0])
	return &v, e
}
func (r screeningRepo) FindCase(ctx context.Context, t, i uuid.UUID) (*c.Case, error) {
	var key uuid.UUID
	e := r.db.QueryRowContext(ctx, "SELECT c.id FROM "+r.table("cases")+" c WHERE c.tenant_id=? AND EXISTS(SELECT 1 FROM "+r.table("case_screenings")+" l WHERE l.tenant_id=c.tenant_id AND l.case_id=c.id AND l.screening_id=?) ORDER BY(c.status='closed'),c.created_at DESC,c.id DESC LIMIT 1", id(t), id(i)).Scan(uid(&key))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	v, e := (caseRepo{r.scope}).Get(ctx, t, key)
	return &v, e
}
func (r screeningRepo) BindContinuous(ctx context.Context, t, s, i uuid.UUID) (bool, error) {
	var prior uuid.UUID
	e := r.db.QueryRowContext(ctx, "SELECT case_id FROM "+r.table("continuous_bindings")+" WHERE tenant_id=? AND screening_id=? FOR UPDATE", id(t), id(s)).Scan(uid(&prior))
	if e == nil {
		if prior != i {
			return false, c.ErrConflict
		}
		return false, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return false, e
	}
	_, e = r.db.ExecContext(ctx, "INSERT INTO "+r.table("continuous_bindings")+"(tenant_id,screening_id,case_id) VALUES(?,?,?)", id(t), id(s), id(i))
	return e == nil, normalized(e)
}
func (r screeningRepo) SyncContinuous(ctx context.Context, t, s, i uuid.UUID, at time.Time) error {
	rows, e := r.db.QueryContext(ctx, "SELECT id,status FROM "+r.schema+".screening_matches WHERE tenant_id=? AND screening_id=? ORDER BY id FOR SHARE", id(t), id(s))
	if e != nil {
		return e
	}
	type match struct {
		key    uuid.UUID
		status string
	}
	matches := []match{}
	for rows.Next() {
		var v match
		if e = rows.Scan(uid(&v.key), &v.status); e != nil {
			rows.Close()
			return e
		}
		matches = append(matches, v)
		if len(matches) > 500 {
			rows.Close()
			return c.Invalid("too many screening matches")
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, v := range matches {
		m := v.key.String()
		if _, e = r.Create(ctx, c.ScreeningLink{ID: uuid.New(), TenantID: t, CaseID: i, ScreeningID: s, MatchID: &m, Status: v.status, CreatedAt: at}); e != nil {
			return e
		}
	}
	return nil
}

type referenceRepo struct{ scope }

func (r referenceRepo) Decisions(ctx context.Context, t uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]c.DecisionReference, error) {
	out := map[uuid.UUID]c.DecisionReference{}
	if len(ids) == 0 {
		return out, nil
	}
	if len(ids) > 500 {
		return nil, c.Invalid("too many decision references")
	}
	a := []any{id(t)}
	for _, v := range ids {
		a = append(a, id(v))
	}
	rows, e := r.db.QueryContext(ctx, "SELECT id,scenario_id,object_type,object_id FROM "+r.schema+".decision_engine_decisions WHERE tenant_id=? AND id IN("+placeholders(len(ids))+") ORDER BY id FOR SHARE", a...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var v c.DecisionReference
		if e = rows.Scan(uid(&v.ID), uid(&v.ScenarioID), &v.ObjectType, &v.ObjectID); e != nil {
			return nil, e
		}
		out[v.ID] = v
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	for _, v := range ids {
		if _, ok := out[v]; !ok {
			return nil, c.ErrNotFound
		}
	}
	return out, nil
}
func (r referenceRepo) Screening(ctx context.Context, t, s uuid.UUID, match string) (c.ScreeningReference, error) {
	var v c.ScreeningReference
	m, e := uuid.Parse(match)
	if e != nil {
		return v, c.Invalid("invalid screening match")
	}
	e = r.db.QueryRowContext(ctx, "SELECT s.id,s.decision_id,m.status FROM "+r.schema+".screening_screenings s JOIN "+r.schema+".screening_matches m ON m.tenant_id=s.tenant_id AND m.screening_id=s.id WHERE s.tenant_id=? AND s.id=? AND m.id=?", id(t), id(s), id(m)).Scan(uid(&v.ID), nullableID{&v.DecisionID}, &v.MatchStatus)
	return v, normalized(e)
}

// Screening lifecycle contracts read authoritative owner rows. Older installs
// without the lifecycle extension still fail closed with owner_unavailable.
func screeningContractError(e error) error {
	var m *driver.MySQLError
	if errors.As(e, &m) && (m.Number == 1146 || m.Number == 1054) {
		return c.ErrUnavailable
	}
	return normalized(e)
}
func (r referenceRepo) ManualScreening(ctx context.Context, t, s, candidate uuid.UUID, actor string, lock bool) (bool, error) {
	user, e := uuid.Parse(actor)
	if e != nil || user == uuid.Nil {
		return false, c.ErrNotFound
	}
	if lock {
		var found uuid.UUID
		if e = r.db.QueryRowContext(ctx, "SELECT id FROM "+r.schema+".screening_screenings WHERE tenant_id=? AND id=? FOR SHARE", id(t), id(s)).Scan(uid(&found)); e != nil {
			return false, screeningContractError(e)
		}
	}
	q := "SELECT s.status='awaiting_review' AND NOT s.is_archived AND m.status='pending' FROM " + r.schema + ".screening_screenings s JOIN " + r.schema + ".screening_matches m ON m.tenant_id=s.tenant_id AND m.screening_id=s.id WHERE s.tenant_id=? AND s.id=? AND m.id=? AND s.creator_id=? AND s.is_manual AND s.decision_id IS NULL AND s.scenario_id IS NULL"
	if lock {
		q += " FOR SHARE OF m"
	}
	var eligible bool
	e = r.db.QueryRowContext(ctx, q, id(t), id(s), id(candidate), id(user)).Scan(&eligible)
	return eligible, screeningContractError(e)
}
func (r referenceRepo) ContinuousScreening(ctx context.Context, t, s uuid.UUID) (*ports.ContinuousScreeningReference, error) {
	event := uuid.NewSHA1(uuid.NameSpaceURL, []byte("screening.requires-review/v1/"+t.String()+"/"+s.String()))
	var out ports.ContinuousScreeningReference
	var inbox mysqlstore.NullUUID
	e := r.db.QueryRowContext(ctx, "SELECT cfg.review_inbox_id,cfg.enabled AND cfg.deleted_at IS NULL,EXISTS(SELECT 1 FROM "+r.schema+".screening_case_event_outbox e WHERE e.tenant_id=run.tenant_id AND e.id=? AND e.kind='requires-review' AND JSON_UNQUOTE(JSON_EXTRACT(e.payload,'$.screening_id'))=BIN_TO_UUID(run.screening_id)) FROM "+r.schema+".screening_continuous_runs run JOIN "+r.schema+".screening_continuous_configs cfg ON cfg.tenant_id=run.tenant_id AND cfg.id=run.config_id WHERE run.tenant_id=? AND run.screening_id=? FOR SHARE OF cfg", id(event), id(t), id(s)).Scan(&inbox, &out.Enabled, &out.Published)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, screeningContractError(e)
	}
	if inbox.Valid {
		v := uuid.UUID(inbox.UUID).String()
		out.ReviewInboxID = &v
	}
	return &out, nil
}
func (r referenceRepo) DecisionScreening(ctx context.Context, t, s uuid.UUID) (*ports.DecisionScreeningReference, error) {
	event := uuid.NewSHA1(uuid.NameSpaceURL, []byte("screening.requires-review/v1/"+t.String()+"/"+s.String()))
	var out ports.DecisionScreeningReference
	var inbox mysqlstore.NullUUID
	var valid bool
	e := r.db.QueryRowContext(ctx, "SELECT s.decision_id,cfg.review_inbox_id,EXISTS(SELECT 1 FROM "+r.schema+".screening_case_event_outbox e WHERE e.tenant_id=s.tenant_id AND e.id=? AND e.kind='requires-review' AND JSON_UNQUOTE(JSON_EXTRACT(e.payload,'$.screening_id'))=BIN_TO_UUID(s.id)),COALESCE(d.id IS NOT NULL AND cfg.id IS NOT NULL AND d.scenario_id=s.scenario_id AND cfg.scenario_id=d.scenario_id AND d.object_type=s.object_type AND d.object_id=s.object_id,FALSE) FROM "+r.schema+".screening_screenings s LEFT JOIN "+r.schema+".decision_engine_decisions d ON d.tenant_id=s.tenant_id AND d.id=s.decision_id LEFT JOIN "+r.schema+".decision_engine_screening_configs cfg ON cfg.tenant_id=s.tenant_id AND cfg.id=s.screening_config_id WHERE s.tenant_id=? AND s.id=? AND s.decision_id IS NOT NULL", id(event), id(t), id(s)).Scan(uid(&out.DecisionID), &inbox, &out.Published, &valid)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, screeningContractError(e)
	}
	if inbox.Valid {
		v := uuid.UUID(inbox.UUID)
		out.ReviewInboxID = &v
	}
	if out.Published && !valid {
		return nil, c.Invalid("screening decision/configuration provenance does not match")
	}
	return &out, nil
}
func (r referenceRepo) ScreeningFile(ctx context.Context, t, caseID, fileID uuid.UUID) (c.File, error) {
	out := c.File{TenantID: t, CaseID: caseID, SourceFileID: &fileID}
	e := r.db.QueryRowContext(ctx, "SELECT f.file_name,f.content_type,f.file_size,f.storage_key,f.uploaded_by FROM "+r.schema+".screening_files f JOIN "+r.schema+".screening_screenings s ON s.tenant_id=f.tenant_id AND s.id=f.screening_id WHERE f.tenant_id=? AND f.id=? AND(EXISTS(SELECT 1 FROM "+r.table("case_screenings")+" l WHERE l.tenant_id=f.tenant_id AND l.case_id=? AND l.screening_id=f.screening_id) OR EXISTS(SELECT 1 FROM "+r.table("case_decisions")+" d WHERE d.tenant_id=f.tenant_id AND d.case_id=? AND d.decision_id=s.decision_id)) FOR SHARE OF f,s", id(t), id(fileID), id(caseID), id(caseID)).Scan(&out.FileName, &out.ContentType, &out.FileSize, &out.StorageKey, &out.UploadedBy)
	return out, screeningContractError(e)
}
