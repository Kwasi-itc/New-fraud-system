package mysql

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// DeliveryInspector is an explicitly tenant-bound, read-only operator adapter.
// Construction is not authorization. The caller must authorize inspection and
// provide a read-only account; no lease token, arbitrary error text or history
// payload is exposed. All queues are included because reservations span queues.
type DeliveryInspector struct {
	db     *sql.DB
	schema string
	tenant uuid.UUID
}

func NewDeliveryInspector(db *sql.DB, database string, tenant uuid.UUID) (*DeliveryInspector, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if db == nil || tenant == uuid.Nil {
		return nil, errors.New("database and authorized tenant required")
	}
	return &DeliveryInspector{db, schema, tenant}, nil
}

type DeliveryInspection struct {
	ID            uuid.UUID  `json:"id"`
	JobID         uuid.UUID  `json:"job_id"`
	Queue         string     `json:"queue"`
	State         string     `json:"state"`
	JobStatus     string     `json:"job_status"`
	Attempt       int        `json:"attempt"`
	MaxAttempts   int        `json:"max_attempts"`
	JobAttempt    int        `json:"job_attempt"`
	CreatedAt     time.Time  `json:"created_at"`
	AvailableAt   time.Time  `json:"available_at"`
	LeaseUntil    *time.Time `json:"lease_until,omitempty"`
	FinalizedAt   *time.Time `json:"finalized_at,omitempty"`
	RetainUntil   *time.Time `json:"retain_until,omitempty"`
	LastErrorCode string     `json:"last_error_code"`
	Reserved      bool       `json:"reserved"`
}

type DeliveryInspectionCursor struct {
	TenantID  uuid.UUID `json:"tenant_id"`
	CreatedAt time.Time `json:"created_at"`
	ID        uuid.UUID `json:"id"`
}

type DeliveryInspectionPage struct {
	Items []DeliveryInspection      `json:"items"`
	Next  *DeliveryInspectionCursor `json:"next,omitempty"`
}

// Page uses the existing tenant/created_at/id index. Selection is bounded before
// joining jobs and reservations, so a page never loads arbitrary history or scans
// a queue subset looking for tenant-owned work. Pages are snapshots, not a frozen
// export: start again from nil to observe later insertions and state changes.
func (r *DeliveryInspector) Page(ctx context.Context, after *DeliveryInspectionCursor, limit int) (DeliveryInspectionPage, error) {
	if limit < 1 || limit > 1000 {
		return DeliveryInspectionPage{}, errors.New("page limit must be 1..1000")
	}
	base := "SELECT id,tenant_id,created_at FROM " + r.schema + ".data_model_index_deliveries WHERE tenant_id=?"
	order := " ORDER BY created_at DESC,id DESC LIMIT ?"
	selection := base + order
	args := []any{mysqlstore.UUID(r.tenant), limit + 1}
	if after != nil {
		if after.TenantID != r.tenant || after.ID == uuid.Nil {
			return DeliveryInspectionPage{}, errors.New("cursor must belong to the inspection tenant")
		}
		at := mysqlstore.DateTime{Time: after.CreatedAt}
		if _, err := at.Value(); err != nil {
			return DeliveryInspectionPage{}, err
		}
		// Separate disjoint ranges avoid a tenant-only index lookup followed by
		// filtering the entire already-seen prefix. Each covering range is
		// independently limited before the bounded merge and detail lookups.
		selection = "SELECT id,tenant_id,created_at FROM ((" + base + " AND created_at=? AND id<?" + order + ") UNION ALL (" + base + " AND created_at<?" + order + ")) AS candidates" + order
		args = []any{mysqlstore.UUID(r.tenant), at, mysqlstore.UUID(after.ID), limit + 1, mysqlstore.UUID(r.tenant), at, limit + 1, limit + 1}
	}
	rows, err := r.db.QueryContext(ctx, r.join(selection)+" ORDER BY d.created_at DESC,d.id DESC", args...)
	if err != nil {
		return DeliveryInspectionPage{}, err
	}
	items := make([]DeliveryInspection, 0, limit+1)
	for rows.Next() {
		item, scanErr := scanDeliveryInspection(rows)
		if scanErr != nil {
			err = scanErr
			break
		}
		items = append(items, item)
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return DeliveryInspectionPage{}, err
	}
	page := DeliveryInspectionPage{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		last := page.Items[limit-1]
		page.Next = &DeliveryInspectionCursor{r.tenant, last.CreatedAt, last.ID}
	}
	return page, nil
}

func (r *DeliveryInspector) Get(ctx context.Context, id uuid.UUID) (DeliveryInspection, error) {
	if id == uuid.Nil {
		return DeliveryInspection{}, errors.New("delivery identity required")
	}
	selection := "SELECT id,tenant_id,created_at FROM " + r.schema + ".data_model_index_deliveries WHERE tenant_id=? AND id=?"
	return scanDeliveryInspection(r.db.QueryRowContext(ctx, r.join(selection), mysqlstore.UUID(r.tenant), mysqlstore.UUID(id)))
}

func (r *DeliveryInspector) join(selection string) string {
	// LEFT JOIN fails explicitly on a missing job instead of silently omitting a
	// retained delivery. The schema normally prevents such orphaned identities.
	return "SELECT d.id,d.job_id,d.queue_name,d.state,j.status,d.attempt,d.max_attempts,j.attempt_count,d.created_at,d.available_at,d.lease_until,d.finalized_at,d.retain_until,d.last_error_code,(v.delivery_id IS NOT NULL) FROM (" + selection + ") AS p JOIN " + r.schema + ".data_model_index_deliveries AS d ON d.tenant_id=p.tenant_id AND d.id=p.id LEFT JOIN " + r.schema + ".data_model_index_jobs AS j ON j.tenant_id=d.tenant_id AND j.id=d.job_id LEFT JOIN " + r.schema + ".data_model_index_delivery_reservations AS v ON v.tenant_id=d.tenant_id AND v.job_id=d.job_id AND v.delivery_id=d.id"
}

func scanDeliveryInspection(row interface{ Scan(...any) error }) (DeliveryInspection, error) {
	var v DeliveryInspection
	var created, available mysqlstore.DateTime
	var lease, finalized, retained mysqlstore.NullDateTime
	if err := row.Scan((*mysqlstore.UUID)(&v.ID), (*mysqlstore.UUID)(&v.JobID), &v.Queue, &v.State, &v.JobStatus, &v.Attempt, &v.MaxAttempts, &v.JobAttempt, &created, &available, &lease, &finalized, &retained, &v.LastErrorCode, &v.Reserved); err != nil {
		return DeliveryInspection{}, err
	}
	v.CreatedAt, v.AvailableAt = created.Time, available.Time
	if lease.Valid {
		at := lease.DateTime.Time
		v.LeaseUntil = &at
	}
	if finalized.Valid {
		at := finalized.DateTime.Time
		v.FinalizedAt = &at
	}
	if retained.Valid {
		at := retained.DateTime.Time
		v.RetainUntil = &at
	}
	return v, nil
}
