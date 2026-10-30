package mysql

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/work"
	"github.com/google/uuid"
)

// QueueInspectionScope requests tenant-wide access to one queue and state. A
// table-scoped submission/cancellation policy must not be reused here. The
// authorizer must verify current tenant status, trusted identity and inspect_work
// permission through bounded local reads in the supplied transaction.
type QueueInspectionScope struct {
	TenantID     uuid.UUID
	Queue, State string
}
type QueueInspectionAuthorizer func(context.Context, *sql.Tx, QueueInspectionScope) error

type WorkInspector struct {
	db        *sql.DB
	schema    string
	tenant    uuid.UUID
	authorize QueueInspectionAuthorizer
}
type WorkInspectionCursor struct {
	QueueInspectionScope
	ID        uuid.UUID
	CreatedAt time.Time
}
type WorkInspectionRequest struct {
	Queue, State string
	Limit        int
	Cursor       *WorkInspectionCursor
}

// WorkInspectionItem deliberately excludes payloads, keys, errors, lease tokens
// and operator actor data. State strings are internal queue values, not HTTP DTOs.
type WorkInspectionItem struct {
	ID, ResourceID         uuid.UUID
	Kind                   string
	Attempt, MaxAttempts   int
	CreatedAt, AvailableAt time.Time
	StartedAt, FinishedAt  *time.Time
}
type WorkInspectionPage struct {
	Items []WorkInspectionItem
	Next  *WorkInspectionCursor
}

func NewWorkInspector(db *sql.DB, database string, tenant uuid.UUID, authorize QueueInspectionAuthorizer) (*WorkInspector, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if db == nil || tenant == uuid.Nil || authorize == nil {
		return nil, errors.New("tenant and explicit queue inspection policy required")
	}
	return &WorkInspector{db: db, schema: s, tenant: tenant, authorize: authorize}, nil
}

func WorkInspectionIndex(database string) (string, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return "", err
	}
	return "ALTER TABLE " + s + ".ingestion_work_jobs ADD INDEX operator_history(tenant_id,queue,state,created_at DESC,id DESC)", nil
}

func InspectionGrants(database, account string) ([]string, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`).MatchString(account) {
		return nil, errors.New("dedicated inspection account required")
	}
	return []string{"GRANT SELECT(id,tenant_id,resource_id,kind,queue,state,attempt,max_attempts,created_at,available_at,started_at,finished_at) ON " + s + ".ingestion_work_jobs TO '" + account + "'@'%'"}, nil
}

// List is a live keyset page, not a snapshot across calls. State transitions can
// move jobs into/out of subsequent pages. Each call reauthorizes the exact scope.
// All queue/state filters are required so one bounded index range serves a page.
func (r *WorkInspector) List(ctx context.Context, in WorkInspectionRequest) (WorkInspectionPage, error) {
	if r == nil || !work.ValidQueueName(in.Queue) || in.Limit < 1 || in.Limit > 100 {
		return WorkInspectionPage{}, errors.New("queue and page size 1..100 required")
	}
	switch in.State {
	case "available", "running", "completed", "discarded", "cancelled":
	default:
		return WorkInspectionPage{}, errors.New("exact work state required")
	}
	scope := QueueInspectionScope{TenantID: r.tenant, Queue: in.Queue, State: in.State}
	if in.Cursor != nil {
		detached := *in.Cursor
		in.Cursor = &detached
		if detached.QueueInspectionScope != scope || detached.ID == uuid.Nil {
			return WorkInspectionPage{}, errors.New("inspection cursor scope mismatch")
		}
		if _, err := (mysqlstore.DateTime{Time: detached.CreatedAt}).Value(); err != nil {
			return WorkInspectionPage{}, err
		}
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: true})
	if err != nil {
		return WorkInspectionPage{}, err
	}
	defer tx.Rollback()
	if err = r.authorize(ctx, tx, scope); err != nil {
		return WorkInspectionPage{}, err
	}
	query, args := r.inspectionQuery(in)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return WorkInspectionPage{}, err
	}
	defer rows.Close()
	page := WorkInspectionPage{Items: make([]WorkInspectionItem, 0, in.Limit)}
	for rows.Next() {
		var item WorkInspectionItem
		var id, resource mysqlstore.UUID
		var created, available mysqlstore.DateTime
		var started, finished mysqlstore.NullDateTime
		if err = rows.Scan(&id, &resource, &item.Kind, &item.Attempt, &item.MaxAttempts, &created, &available, &started, &finished); err != nil {
			return WorkInspectionPage{}, err
		}
		item.ID = uuid.UUID(id)
		item.ResourceID = uuid.UUID(resource)
		item.CreatedAt = created.Time
		item.AvailableAt = available.Time
		if started.Valid {
			at := started.DateTime.Time
			item.StartedAt = &at
		}
		if finished.Valid {
			at := finished.DateTime.Time
			item.FinishedAt = &at
		}
		if len(page.Items) == in.Limit {
			last := page.Items[in.Limit-1]
			page.Next = &WorkInspectionCursor{QueueInspectionScope: scope, ID: last.ID, CreatedAt: last.CreatedAt}
		} else {
			page.Items = append(page.Items, item)
		}
	}
	if err = rows.Err(); err != nil {
		return WorkInspectionPage{}, err
	}
	if err = rows.Close(); err != nil {
		return WorkInspectionPage{}, err
	}
	if err = tx.Commit(); err != nil {
		return WorkInspectionPage{}, err
	}
	return page, nil
}

func (r *WorkInspector) inspectionQuery(in WorkInspectionRequest) (string, []any) {
	// The unhinted mixed-history plan chose ready and sorted the whole match.
	// Require the owner installation index to keep page access scoped and ordered.
	q := "SELECT id,resource_id,kind,attempt,max_attempts,created_at,available_at,started_at,finished_at FROM " + r.schema + ".ingestion_work_jobs FORCE INDEX (operator_history) WHERE tenant_id=? AND queue=? AND state=?"
	args := []any{mysqlstore.UUID(r.tenant), in.Queue, in.State}
	if in.Cursor != nil {
		q += " AND (created_at<? OR (created_at=? AND id<?))"
		at := mysqlstore.DateTime{Time: in.Cursor.CreatedAt}
		args = append(args, at, at, mysqlstore.UUID(in.Cursor.ID))
	}
	return q + " ORDER BY created_at DESC,id DESC LIMIT ?", append(args, in.Limit+1)
}
