package mysql

import (
	"context"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type CustomListSummary struct {
	platform.CustomList
	EntryCount int64
}

func (r CustomListRepository) listSummarySQL() string {
	return "SELECT l.id,l.tenant_id,l.created_at,l.updated_at," + r.projection("l.name", "l.description", "l.kind") +
		",(SELECT COUNT(*) FROM " + r.schema + ".decision_engine_custom_list_entries e WHERE e.tenant_id=l.tenant_id AND e.list_id=l.id)" +
		" FROM " + r.schema + ".decision_engine_custom_lists l WHERE l.tenant_id=? ORDER BY l.updated_at DESC,l.created_at DESC,l.id DESC"
}

// Summaries reads bounded list metadata and indexed counts in the same authorized
// snapshot. Entry values are never loaded to render the list overview.
func (s *CustomListAdmin) Summaries(ctx context.Context) ([]CustomListSummary, error) {
	return listAdminRun(ctx, s, false, func(r CustomListRepository) ([]CustomListSummary, error) {
		args, err := r.ids(r.tenant.String())
		if err != nil {
			return nil, err
		}
		return platformList(ctx, r.platformRepository, r.listSummarySQL(), args, func(row rowScanner) (CustomListSummary, int64, error) {
			var id, tenant mysqlstore.UUID
			var created, updated mysqlstore.DateTime
			var name, description, kind mysqlstore.Text
			var size, count int64
			if err := row.Scan(&id, &tenant, &created, &updated, &size, &name, &description, &kind, &count); err != nil {
				return CustomListSummary{}, 0, err
			}
			return CustomListSummary{CustomList: platform.CustomList{ID: uuid.UUID(id).String(), TenantID: uuid.UUID(tenant).String(), Name: string(name), Description: string(description), Kind: string(kind), CreatedAt: created.Time, UpdatedAt: updated.Time}, EntryCount: count}, size, nil
		})
	})
}
