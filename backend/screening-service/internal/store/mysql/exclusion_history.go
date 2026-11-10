package mysql

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	pg "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/postgres"
	"github.com/google/uuid"
	"time"
)

type historyCursor struct {
	At time.Time `json:"at"`
	ID string    `json:"id"`
}

func encodeCursor(at time.Time, id string) string {
	b, _ := json.Marshal(historyCursor{at, id})
	return base64.RawURLEncoding.EncodeToString(b)
}
func (s *Store) CaseExclusions(ctx context.Context, p identity.Principal, tenant, caseID uuid.UUID, operation, cursor string, limit int) (out pg.CaseExclusionPage, err error) {
	out = pg.CaseExclusionPage{Events: []pg.CaseExclusionEvent{}, Applications: []pg.ExclusionApplication{}}
	if limit < 1 || limit > 50 || len(cursor) > 2048 {
		return out, identity.Invalid("Invalid history pagination.")
	}
	var after historyCursor
	if cursor != "" {
		raw, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil || json.Unmarshal(raw, &after) != nil || after.At.IsZero() {
			return out, identity.Invalid("Invalid history cursor.")
		}
		if _, e = uuid.Parse(after.ID); e != nil {
			return out, identity.Invalid("Invalid history cursor.")
		}
	}
	if operation != "" {
		if _, e := uuid.Parse(operation); e != nil {
			return out, identity.Invalid("Invalid exclusion operation.")
		}
	}
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	r := s.InTransaction(tx)
	if e = r.CheckIdentity(ctx, p, true); e != nil {
		return out, e
	}
	if _, _, e = r.caseAuthority(ctx, p, tenant, caseID, "", false); e != nil {
		return out, e
	}
	args := []any{mysqlstore.UUID(tenant), mysqlstore.UUID(caseID)}
	if operation == "" {
		q := "SELECT e.operation_id,e.match_id,e.object_type,e.object_id,e.provider,e.entity_id,e.actor_id,e.findings,e.active,x.active,r.created_at FROM " + r.table("reviews") + " r JOIN " + r.table("object_exclusion_events") + " e ON e.tenant_id=r.tenant_id AND e.operation_id=r.operation_id JOIN " + r.table("object_exclusions") + " x ON x.tenant_id=e.tenant_id AND x.scope_hash=e.scope_hash AND x.entity_id=e.entity_id WHERE r.tenant_id=? AND r.case_id=?"
		if !after.At.IsZero() {
			q += " AND(r.created_at<? OR(r.created_at=? AND r.operation_id<?))"
			args = append(args, after.At, after.At, id(after.ID))
		}
		q += " ORDER BY r.created_at DESC,r.operation_id DESC LIMIT ?"
		args = append(args, limit+1)
		rows, e := r.q.QueryContext(ctx, q, args...)
		if e != nil {
			return out, normalize(e)
		}
		for rows.Next() {
			var v pg.CaseExclusionEvent
			if e = rows.Scan(uid(&v.OperationID), uid(&v.MatchID), &v.ObjectType, &v.ObjectID, &v.Provider, &v.EntityID, uid(&v.ActorID), &v.Findings, &v.Activated, &v.Active, &v.CreatedAt); e != nil {
				rows.Close()
				return out, normalize(e)
			}
			out.Events = append(out.Events, v)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return out, e
		}
		if len(out.Events) > limit {
			out.Events = out.Events[:limit]
			last := out.Events[limit-1]
			out.NextCursor = encodeCursor(last.CreatedAt, last.OperationID)
		}
	} else {
		var belongs bool
		e = r.q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+r.table("reviews")+" r JOIN "+r.table("object_exclusion_events")+" e ON e.tenant_id=r.tenant_id AND e.operation_id=r.operation_id WHERE r.tenant_id=? AND r.case_id=? AND r.operation_id=?)", mysqlstore.UUID(tenant), mysqlstore.UUID(caseID), id(operation)).Scan(&belongs)
		if e != nil {
			return out, normalize(e)
		}
		if !belongs {
			return out, identity.Forbidden()
		}
		q := "SELECT a.screening_id,a.match_id,a.applied_at FROM " + r.table("object_exclusion_applications") + " a WHERE a.tenant_id=? AND a.operation_id=? AND EXISTS(SELECT 1 FROM " + r.schema + ".case_manager_case_screenings l WHERE l.tenant_id=a.tenant_id AND l.case_id=? AND l.screening_id=a.screening_id AND l.match_id=a.match_id)"
		args = []any{mysqlstore.UUID(tenant), id(operation), mysqlstore.UUID(caseID)}
		if !after.At.IsZero() {
			q += " AND(a.applied_at<? OR(a.applied_at=? AND a.match_id<?))"
			args = append(args, after.At, after.At, id(after.ID))
		}
		q += " ORDER BY a.applied_at DESC,a.match_id DESC LIMIT ?"
		args = append(args, limit+1)
		rows, e := r.q.QueryContext(ctx, q, args...)
		if e != nil {
			return out, normalize(e)
		}
		for rows.Next() {
			var v pg.ExclusionApplication
			if e = rows.Scan(uid(&v.ScreeningID), uid(&v.MatchID), &v.AppliedAt); e != nil {
				rows.Close()
				return out, normalize(e)
			}
			out.Applications = append(out.Applications, v)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return out, e
		}
		if len(out.Applications) > limit {
			out.Applications = out.Applications[:limit]
			last := out.Applications[limit-1]
			out.NextCursor = encodeCursor(last.AppliedAt, last.MatchID)
		}
	}
	return out, tx.Commit()
}
