package mysql

import (
	"context"
	"database/sql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// EnsureRepair durably schedules a detected managed-index gap. An existing live
// job is not duplicated; terminal jobs get a fresh delivery reservation atomically.
// Detection is advisory: the worker must verify drift again before applying DDL.
func (a *IndexJobApplication) EnsureRepair(ctx context.Context, table uuid.UUID, columns []string) (uuid.UUID, bool, error) {
	if e := datamodel.ValidateIndexJobCreate(datamodel.IndexJobTypeRepair, columns); e != nil {
		return uuid.Nil, false, e
	}
	normalized := make([]string, len(columns))
	for i, c := range columns {
		normalized[i] = datamodel.NormalizeName(c)
	}
	if len(normalized) > 100 {
		return uuid.Nil, false, ErrSnapshotLimit
	}
	key := datamodel.BuildIndexJobDedupeKey(a.manager.tenant, table, datamodel.IndexJobTypeRepair, normalized)
	schema, e := mysqlstore.Identifier(a.manager.database)
	if e != nil {
		return uuid.Nil, false, e
	}
	var id uuid.UUID
	scheduled := false
	err := a.manager.RunWithTx(ctx, func(s ports.MutationStore, tx *sql.Tx) error {
		var status string
		if e := tx.QueryRowContext(ctx, "SELECT status FROM "+schema+".data_model_tenants WHERE id=? FOR SHARE", mysqlstore.UUID(a.manager.tenant)).Scan(&status); e != nil {
			return e
		}
		if status != "active" {
			return ErrModelUnavailable
		}
		var got mysqlstore.UUID
		if e := tx.QueryRowContext(ctx, "SELECT id FROM "+schema+".data_model_tables WHERE tenant_id=? AND id=? AND NOT archived FOR UPDATE", mysqlstore.UUID(a.manager.tenant), mysqlstore.UUID(table)).Scan(&got); e != nil {
			return e
		}
		fields, e := s.Fields().ListByTable(ctx, table)
		if e != nil {
			return e
		}
		active := map[string]bool{}
		for _, field := range fields {
			active[field.Name] = !field.Archived
		}
		for _, column := range normalized {
			if !active[column] {
				return sql.ErrNoRows
			}
		}
		existing, e := s.IndexJobs().GetByDedupeKeyForUpdate(ctx, key)
		if e != nil {
			return e
		}
		if existing == nil {
			v, e := a.owner(s).Create(ctx, service.CreateIndexJobInput{TenantID: a.manager.tenant, TableID: table, IndexType: datamodel.IndexJobTypeRepair, Columns: normalized, RequestedByOperation: "reconcile_repair"})
			id = v.ID
			scheduled = e == nil
			return e
		}
		id = existing.ID
		if existing.Status == datamodel.IndexJobStatusPending || existing.Status == datamodel.IndexJobStatusRunning {
			return nil
		}
		if e = a.manager.queue.prepareOwnerRetryTx(ctx, tx, a.manager.tenant, id); e != nil {
			return e
		}
		now := a.clock.Now()
		if e = s.IndexJobs().Retry(ctx, id, now); e != nil {
			return e
		}
		if e = s.EnqueueIndexJob(ctx, id, &now); e != nil {
			return e
		}
		scheduled = true
		return s.SchemaChanges().Create(ctx, datamodel.SchemaChange{ID: a.ids.New(), TenantID: a.manager.tenant, Operation: "request_index_job", ResourceType: "index_job", ResourceID: id, Status: "applied", Details: []byte(`{"reason":"missing_managed_index"}`), CreatedAt: now})
	})
	return id, scheduled, err
}
