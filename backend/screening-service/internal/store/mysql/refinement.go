package mysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/mysqlcase"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	d "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	gomysql "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"strings"
	"time"
)

type RefinementContext struct {
	SourceID  string          `json:"source_id"`
	Version   string          `json:"version"`
	CanRefine bool            `json:"can_refine"`
	Reason    string          `json:"reason,omitempty"`
	Provider  string          `json:"provider"`
	Queries   []d.SearchQuery `json:"queries"`
}
type RefinementCommand struct {
	OperationID     uuid.UUID `json:"operation_id"`
	ReplacementID   uuid.UUID `json:"replacement_id"`
	ExpectedVersion string    `json:"expected_version"`
	Findings        string    `json:"findings"`
}

func (s *Store) IsScreeningSuperseded(ctx context.Context, tenant, source string) (bool, error) {
	var found bool
	e := s.q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+s.table("refinements")+" WHERE tenant_id=? AND source_id=? AND applied_at IS NOT NULL)", id(tenant), id(source)).Scan(&found)
	return found, normalize(e)
}
func (s *Store) refinementAuthority(ctx context.Context, p identity.Principal, t, c, source uuid.UUID, write bool) (bool, error) {
	if err := s.CheckIdentity(ctx, p, write); err != nil {
		return false, err
	}
	status, active, e := s.caseAuthority(ctx, p, t, c, "", write)
	if e != nil {
		return false, e
	}
	var linked bool
	e = s.q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+s.schema+".case_manager_case_screenings WHERE tenant_id=? AND case_id=? AND screening_id=?)", mysqlstore.UUID(t), mysqlstore.UUID(c), mysqlstore.UUID(source)).Scan(&linked)
	if e != nil {
		return false, e
	}
	if !linked {
		return false, identity.Forbidden()
	}
	return active && status != "closed", nil
}
func (s *Store) RefinementContext(ctx context.Context, p identity.Principal, t, c, source uuid.UUID) (out RefinementContext, err error) {
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	r := s.InTransaction(tx)
	open, e := r.refinementAuthority(ctx, p, t, c, source, false)
	if e != nil {
		return out, e
	}
	item, e := r.Screenings().GetByID(ctx, t.String(), source.String())
	if e != nil {
		return out, e
	}
	superseded, e := r.IsScreeningSuperseded(ctx, t.String(), source.String())
	if e != nil {
		return out, e
	}
	out = RefinementContext{SourceID: source.String(), Version: item.UpdatedAt.UTC().Format(time.RFC3339Nano), CanRefine: open && !superseded && !item.IsArchived && (item.Status == d.StatusAwaitingReview || item.Status == d.StatusNoHit || item.Status == d.StatusFailed), Provider: item.Provider, Queries: []d.SearchQuery{}}
	var request d.SearchRequest
	if json.Unmarshal(item.RequestJSON, &request) != nil {
		return out, identity.Invalid("Saved screening query is unavailable.")
	}
	out.Queries = request.Queries
	if !out.CanRefine {
		out.Reason = "Refinement requires an open case and a current, unresolved screening. Confirmed matches cannot be cleared by refinement."
	}
	return out, tx.Commit()
}
func (s *Store) ApplyRefinement(ctx context.Context, p identity.Principal, t, c, source uuid.UUID, cmd RefinementCommand) (replacement string, err error) {
	cmd.Findings = strings.TrimSpace(cmd.Findings)
	if cmd.OperationID == uuid.Nil || cmd.ReplacementID == uuid.Nil || cmd.ReplacementID == source || cmd.ExpectedVersion == "" || len(cmd.Findings) < 1 || len(cmd.Findings) > 2000 {
		return "", identity.Invalid("Operation, replacement search, source version and findings (1-2000 bytes) are required.")
	}
	raw, _ := json.Marshal(struct {
		Command              RefinementCommand
		Actor                string
		Tenant, Case, Source uuid.UUID
	}{cmd, p.Subject, t, c, source})
	digest := sha256.Sum256(raw)
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	r := s.InTransaction(tx)
	open, e := r.refinementAuthority(ctx, p, t, c, source, true)
	if e != nil {
		return "", e
	}
	var oldHash []byte
	var oldResult mysqlstore.UUID
	e = r.q.QueryRowContext(ctx, "SELECT payload_hash,screening_id FROM "+s.table("refinements")+" WHERE tenant_id=? AND id=? AND actor_id=?", mysqlstore.UUID(t), mysqlstore.UUID(cmd.OperationID), id(p.Subject)).Scan(&oldHash, &oldResult)
	if e == nil {
		if string(oldHash) != string(digest[:]) {
			return "", identity.Conflict()
		}
		return uuid.UUID(oldResult).String(), tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return "", e
	}
	if !open {
		return "", identity.Invalid("Reopen the case before applying a refinement.")
	}
	root := source
	var priorRoot mysqlstore.UUID
	e = r.q.QueryRowContext(ctx, "SELECT root_id FROM "+s.table("refinements")+" WHERE tenant_id=? AND screening_id=? AND applied_at IS NOT NULL", mysqlstore.UUID(t), mysqlstore.UUID(source)).Scan(&priorRoot)
	if e == nil {
		root = uuid.UUID(priorRoot)
	} else if !errors.Is(e, sql.ErrNoRows) {
		return "", e
	}
	if _, e = r.Screenings().Lock(ctx, t.String(), root.String()); e != nil {
		return "", e
	}
	parent, e := r.Screenings().Lock(ctx, t.String(), source.String())
	if e != nil {
		return "", e
	}
	superseded, e := r.IsScreeningSuperseded(ctx, t.String(), source.String())
	if e != nil {
		return "", e
	}
	if superseded || parent.IsArchived || parent.UpdatedAt.UTC().Format(time.RFC3339Nano) != cmd.ExpectedVersion || parent.Status == d.StatusConfirmedHit || (parent.Status != d.StatusAwaitingReview && parent.Status != d.StatusNoHit && parent.Status != d.StatusFailed) {
		return "", identity.Conflict()
	}
	var depth int
	if e = r.q.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+s.table("refinements")+" WHERE tenant_id=? AND root_id=?", mysqlstore.UUID(t), mysqlstore.UUID(root)).Scan(&depth); e != nil {
		return "", e
	}
	if depth >= 20 {
		return "", identity.Invalid("This screening has reached its refinement limit.")
	}
	next, e := r.Screenings().Lock(ctx, t.String(), cmd.ReplacementID.String())
	if e != nil {
		return "", e
	}
	var creator mysqlstore.UUID
	e = r.q.QueryRowContext(ctx, "SELECT creator_id FROM "+s.table("screenings")+" WHERE tenant_id=? AND id=?", mysqlstore.UUID(t), mysqlstore.UUID(cmd.ReplacementID)).Scan(&creator)
	if e != nil {
		return "", e
	}
	if !next.IsManual || next.IsArchived || uuid.UUID(creator).String() != p.Subject || next.Provider != parent.Provider || next.Partial || (next.Status != d.StatusNoHit && next.Status != d.StatusAwaitingReview) {
		return "", identity.Invalid("Use a completed, unreviewed search you created with the same provider and complete results.")
	}
	if next.CreatedAt.Before(parent.UpdatedAt) || next.CreatedAt.Before(time.Now().UTC().Add(-15*time.Minute)) {
		return "", identity.Invalid("Create a fresh preview after loading the current screening before applying changes.")
	}
	var used, reviewed bool
	e = r.q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+s.schema+".case_manager_case_screenings WHERE tenant_id=? AND screening_id=?),EXISTS(SELECT 1 FROM "+s.table("matches")+" WHERE tenant_id=? AND screening_id=? AND (status<>'pending' OR ?='no_hit'))", mysqlstore.UUID(t), mysqlstore.UUID(cmd.ReplacementID), mysqlstore.UUID(t), mysqlstore.UUID(cmd.ReplacementID), next.Status).Scan(&used, &reviewed)
	if e != nil {
		return "", e
	}
	if used || reviewed {
		return "", identity.Invalid("Use a fresh preview that has not been reviewed or attached to another investigation.")
	}
	if pending, confirmed, e := r.ScreeningMatches().ReviewCounts(ctx, t.String(), next.ID); e != nil {
		return "", e
	} else if confirmed > 0 || (next.Status == d.StatusAwaitingReview && pending == 0) {
		return "", identity.Invalid("Reviewed search results cannot replace a screening.")
	}
	_, e = r.q.ExecContext(ctx, "INSERT INTO "+s.table("refinements")+"(tenant_id,id,case_id,root_id,source_id,screening_id,actor_id,payload_hash,findings,source_version,created_at,applied_at) VALUES(?,?,?,?,?,?,?,?,?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))", mysqlstore.UUID(t), mysqlstore.UUID(cmd.OperationID), mysqlstore.UUID(c), mysqlstore.UUID(root), mysqlstore.UUID(source), mysqlstore.UUID(cmd.ReplacementID), id(p.Subject), digest[:], cmd.Findings, parent.UpdatedAt)
	if e != nil {
		var duplicate *gomysql.MySQLError
		if errors.As(e, &duplicate) && duplicate.Number == 1062 {
			return "", identity.Conflict()
		}
		return "", e
	}
	if e = mysqlcase.RecordScreeningRefinement(ctx, tx, strings.Trim(s.schema, "`"), t, cmd.OperationID, uuid.MustParse(p.Subject), p.SessionID, p.AuthzVersion); e != nil {
		return "", e
	}
	return next.ID, tx.Commit()
}
