package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// History is scoped to one authorized case. It never projects evidence from
// other cases, even when they share the same exclusion scope.
type CaseExclusionEvent struct {
	OperationID string    `json:"operation_id"`
	MatchID     string    `json:"match_id"`
	ObjectType  string    `json:"object_type"`
	ObjectID    string    `json:"object_id"`
	Provider    string    `json:"provider"`
	EntityID    string    `json:"entity_id"`
	ActorID     string    `json:"actor_id"`
	Findings    string    `json:"findings"`
	Activated   bool      `json:"activated"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"created_at"`
}
type ExclusionApplication struct {
	ScreeningID string    `json:"screening_id"`
	MatchID     string    `json:"match_id"`
	AppliedAt   time.Time `json:"applied_at"`
}
type CaseExclusionPage struct {
	Events       []CaseExclusionEvent   `json:"events"`
	Applications []ExclusionApplication `json:"applications"`
	NextCursor   string                 `json:"next_cursor"`
}
type exclusionCursor struct {
	At time.Time `json:"at"`
	ID string    `json:"id"`
}

func historyCursor(raw string, application bool) (exclusionCursor, error) {
	var c exclusionCursor
	if raw == "" {
		return c, nil
	}
	if len(raw) > 2048 {
		return c, identity.Invalid("Invalid history cursor.")
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || json.Unmarshal(data, &c) != nil || c.At.IsZero() || c.ID == "" || len(c.ID) > 1024 {
		return c, identity.Invalid("Invalid history cursor.")
	}
	if !application {
		if _, err := uuid.Parse(c.ID); err != nil {
			return c, identity.Invalid("Invalid history cursor.")
		}
	}
	return c, nil
}
func encodeHistoryCursor(at time.Time, id string) string {
	raw, _ := json.Marshal(exclusionCursor{At: at, ID: id})
	return base64.RawURLEncoding.EncodeToString(raw)
}

// ReadCaseExclusions uses the same identity -> case -> inbox lock order as
// review. SHARE locks protect access while the bounded page is read; no network
// calls or owner mutations occur in this transaction.
func ReadCaseExclusions(ctx context.Context, db *pgxpool.Pool, p identity.Principal, tenant, caseID uuid.UUID, operation, cursor string, limit int) (CaseExclusionPage, error) {
	out := CaseExclusionPage{Events: []CaseExclusionEvent{}, Applications: []ExclusionApplication{}}
	if p.Subject == "" || p.TenantID != tenant || tenant == uuid.Nil || caseID == uuid.Nil {
		return out, identity.Forbidden()
	}
	if limit < 1 || limit > 50 {
		return out, identity.Invalid("History limit must be between 1 and 50.")
	}
	if operation != "" {
		if _, err := uuid.Parse(operation); err != nil {
			return out, identity.Invalid("Invalid exclusion operation.")
		}
	}
	after, err := historyCursor(cursor, operation != "")
	if err != nil {
		return out, err
	}
	var at any
	if !after.At.IsZero() {
		at = after.At
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if err := identity.CheckIdentity(ctx, tx, p, true); err != nil {
		return out, err
	}
	var inbox uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT inbox_id FROM case_manager.cases WHERE tenant_id=$1 AND id=$2 FOR SHARE`, tenant, caseID).Scan(&inbox); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return out, identity.Forbidden()
		}
		return out, err
	}
	var inboxStatus string
	if err := tx.QueryRow(ctx, `SELECT status FROM case_manager.inboxes WHERE tenant_id=$1 AND id=$2 FOR SHARE`, tenant, inbox).Scan(&inboxStatus); err != nil {
		return out, err
	}
	if !p.Admin {
		var member bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM case_manager.inbox_users WHERE tenant_id=$1 AND inbox_id=$2 AND user_id=$3)`, tenant, inbox, p.Subject).Scan(&member); err != nil {
			return out, err
		}
		if !member {
			return out, identity.Forbidden()
		}
	}
	// Closed cases/inactive inboxes may be read with current access, but the
	// existing review endpoint still refuses all new mutations.
	if operation == "" {
		rows, err := tx.Query(ctx, `SELECT e.operation_id::text,e.match_id,e.object_type,e.object_id,e.provider,e.entity_id,e.actor_id,e.findings,e.active,x.active,r.created_at
FROM screening.investigator_reviews r
JOIN screening.object_exclusion_events e ON e.tenant_id=r.tenant_id AND e.operation_id=r.operation_id
JOIN screening.object_exclusions x ON (x.tenant_id,x.object_type,x.object_id,x.provider,x.query_fingerprint,x.entity_id) =
 (e.tenant_id,e.object_type,e.object_id,e.provider,e.query_fingerprint,e.entity_id)
WHERE r.tenant_id=$1 AND r.case_id=$2
AND ($3::timestamptz IS NULL OR (r.created_at,r.operation_id)<($3,$4::uuid))
ORDER BY r.created_at DESC,r.operation_id DESC LIMIT $5`, tenant, caseID, at, nullIfEmpty(after.ID), limit+1)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var e CaseExclusionEvent
			if err := rows.Scan(&e.OperationID, &e.MatchID, &e.ObjectType, &e.ObjectID, &e.Provider, &e.EntityID, &e.ActorID, &e.Findings, &e.Activated, &e.Active, &e.CreatedAt); err != nil {
				rows.Close()
				return out, err
			}
			out.Events = append(out.Events, e)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
		if len(out.Events) > limit {
			out.Events = out.Events[:limit]
			last := out.Events[limit-1]
			out.NextCursor = encodeHistoryCursor(last.CreatedAt, last.OperationID)
		}
	} else {
		var belongs bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM screening.investigator_reviews r JOIN screening.object_exclusion_events e ON e.tenant_id=r.tenant_id AND e.operation_id=r.operation_id WHERE r.tenant_id=$1 AND r.case_id=$2 AND r.operation_id=$3)`, tenant, caseID, operation).Scan(&belongs); err != nil {
			return out, err
		}
		if !belongs {
			return out, identity.Forbidden()
		}
		rows, err := tx.Query(ctx, `SELECT a.screening_id::text,a.match_id,a.applied_at
FROM screening.object_exclusion_applications a
WHERE a.tenant_id=$1 AND a.operation_id=$3
AND EXISTS(SELECT 1 FROM case_manager.case_screenings l WHERE l.tenant_id=a.tenant_id AND l.case_id=$2 AND l.screening_id=a.screening_id AND l.match_id=a.match_id)
AND ($4::timestamptz IS NULL OR (a.applied_at,a.match_id)<($4,$5::text))
ORDER BY a.applied_at DESC,a.match_id DESC LIMIT $6`, tenant, caseID, operation, at, after.ID, limit+1)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var a ExclusionApplication
			if err := rows.Scan(&a.ScreeningID, &a.MatchID, &a.AppliedAt); err != nil {
				rows.Close()
				return out, err
			}
			out.Applications = append(out.Applications, a)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
		if len(out.Applications) > limit {
			out.Applications = out.Applications[:limit]
			last := out.Applications[limit-1]
			out.NextCursor = encodeHistoryCursor(last.AppliedAt, last.MatchID)
		}
	}
	return out, tx.Commit(ctx)
}
