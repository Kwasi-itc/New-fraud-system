package mysql

import (
	"context"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// Cursors are positions, not authority. Every page is tenant authorized again.
// Newer events inserted after a page are visible when restarting from page one.
type PublicationPageCursor struct {
	Version    int       `json:"v"`
	TenantID   uuid.UUID `json:"tenant_id"`
	ScenarioID uuid.UUID `json:"scenario_id"`
	CreatedAt  time.Time `json:"created_at"`
	ID         uuid.UUID `json:"id"`
}
type PublicationPageFilter struct {
	Limit int
	After *PublicationPageCursor
}
type PublicationPage struct {
	Items []scenario.Publication
	Next  *PublicationPageCursor
}

func (r ScenarioPublicationRepository) Page(ctx context.Context, tenant, parent string, f PublicationPageFilter) (PublicationPage, error) {
	args, err := r.ids(tenant, parent)
	if err != nil {
		return PublicationPage{}, err
	}
	if f.Limit < 1 || f.Limit > 500 || f.Limit > r.maxRows {
		return PublicationPage{}, ErrExecutionPage
	}
	where := " WHERE tenant_id=? AND scenario_id=?"
	if c := f.After; c != nil {
		if c.Version != 1 || c.TenantID.String() != tenant || c.ScenarioID.String() != parent || c.ID == uuid.Nil || c.CreatedAt.IsZero() || !c.CreatedAt.Equal(c.CreatedAt.UTC().Truncate(time.Microsecond)) {
			return PublicationPage{}, ErrExecutionPage
		}
		at := mysqlstore.DateTime{Time: c.CreatedAt}
		if _, err := at.Value(); err != nil {
			return PublicationPage{}, ErrExecutionPage
		}
		where += " AND (created_at<? OR (created_at=? AND id<?))"
		args = append(args, at, at, mysqlstore.UUID(c.ID))
	}
	rows, err := r.tx.QueryContext(ctx, r.selectSQL()+where+" ORDER BY created_at DESC,id DESC LIMIT ?", append(args, f.Limit+1)...)
	if err != nil {
		return PublicationPage{}, err
	}
	defer rows.Close()
	out := PublicationPage{Items: make([]scenario.Publication, 0, f.Limit)}
	used := int64(0)
	for rows.Next() {
		if len(out.Items) == f.Limit {
			last := out.Items[len(out.Items)-1]
			out.Next = &PublicationPageCursor{1, r.tenant, uuid.MustParse(parent), last.CreatedAt, uuid.MustParse(last.ID)}
			break
		}
		value, size, err := r.scan(rows)
		if err != nil {
			return PublicationPage{}, err
		}
		if size < 0 || size > int64(r.maxBytes)-used-128 {
			return PublicationPage{}, ErrProjectionLimit
		}
		used += size + 128
		out.Items = append(out.Items, value)
	}
	if err := rows.Err(); err != nil {
		return PublicationPage{}, err
	}
	return out, nil
}
