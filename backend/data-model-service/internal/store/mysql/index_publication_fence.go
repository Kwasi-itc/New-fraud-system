package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/google/uuid"
)

type publishedIndexFence struct {
	table, version uuid.UUID
	managed        bool
}

// WithPublishedNativeIndex binds completion to the version returned by physical
// publication. It does not execute DDL or grant authority. Applied completion
// locks registry -> job -> delivery and verifies the actual full native index
// before atomically writing the outcome, audit and delivery acknowledgement.
// Text indexing requires its own strategy and is explicitly unsupported here.
func (s *LeasedIndexJobExecutionStore) WithPublishedNativeIndex(table, version uuid.UUID) (*LeasedIndexJobExecutionStore, error) {
	if s == nil || s.delivery == nil || table == uuid.Nil || version == uuid.Nil {
		return nil, errors.New("leased execution and published table/version required")
	}
	copy := *s
	copy.publication = &publishedIndexFence{table: table, version: version}
	return &copy, nil
}

func (s *LeasedIndexJobExecutionStore) WithPublishedManagedIndex(table, version uuid.UUID) (*LeasedIndexJobExecutionStore, error) {
	copy, err := s.WithPublishedNativeIndex(table, version)
	if err != nil {
		return nil, err
	}
	copy.publication.managed = true
	return copy, nil
}

func (s *LeasedIndexJobExecutionStore) verifyPublishedIndex(ctx context.Context, tx *sql.Tx, physical PhysicalTable) error {
	r := metadataRepository{tx, s.delivery.schema, s.lease.TenantID, 1000, s.maxBytes}
	job, err := (IndexJobRepository{r}).GetByID(ctx, s.lease.JobID)
	if err != nil {
		return err
	}
	if job.TableID == nil || *job.TableID != physical.TableID || (!s.publication.managed && job.IndexType != datamodel.IndexJobTypeSearch) {
		return ErrPhysicalConflict
	}
	if err := datamodel.ValidateIndexJobCreate(job.IndexType, job.Columns); err != nil {
		return err
	}
	// Bounded table-scoped metadata reads, followed by one physical index read.
	table, err := (TableRepository{r}).GetByID(ctx, physical.TableID)
	if err != nil {
		return err
	}
	if table.Archived || table.Name != job.TableName {
		return ErrPhysicalConflict
	}
	fields, err := (FieldRepository{r}).ListByTable(ctx, physical.TableID)
	if err != nil {
		return err
	}
	mappings, err := (PhysicalRegistry{r}).Fields(ctx, physical.TableID)
	if err != nil {
		return err
	}
	byName := make(map[string]datamodel.Field, len(fields))
	components := map[uuid.UUID]map[string]string{}
	for _, f := range fields {
		byName[f.Name] = f
	}
	for _, m := range mappings {
		if components[m.FieldID] == nil {
			components[m.FieldID] = map[string]string{}
		}
		components[m.FieldID][m.Component] = m.Name
	}
	var wanted []string
	var prefixes []int
	for _, name := range job.Columns {
		f, ok := byName[name]
		if !ok || f.Archived {
			return ErrPhysicalConflict
		}
		if f.DataType == datamodel.DataTypeString && !s.publication.managed {
			return ErrPhysicalPlanningUnsupported
		}
		parts := []string{"value"}
		if f.DataType == datamodel.DataTypeIPAddress {
			parts = []string{"family", "address", "prefix"}
		}
		for _, part := range parts {
			column := components[f.ID][part]
			if column == "" {
				return ErrPhysicalConflict
			}
			wanted = append(wanted, column)
			prefixes = append(prefixes, 0)
		}
	}
	if s.publication.managed {
		mapped := make([]MappedPhysicalField, 0, len(fields))
		for _, f := range fields {
			v := MappedPhysicalField{Field: f}
			for _, c := range mappings {
				if c.FieldID == f.ID {
					v.Components = append(v.Components, c)
				}
			}
			mapped = append(mapped, v)
		}
		planner, err := NewPhysicalPlanner(s.delivery.schema[1:len(s.delivery.schema)-1], s.lease.TenantID, physical, mapped)
		if err != nil {
			return err
		}
		ids := make([]uuid.UUID, len(job.Columns))
		for i, name := range job.Columns {
			ids[i] = byName[name].ID
		}
		parts, err := planner.ManagedIndexParts(ids)
		if err != nil {
			return err
		}
		wanted, prefixes = nil, nil
		for _, part := range parts {
			wanted = append(wanted, part.Column)
			prefixes = append(prefixes, part.Prefix)
		}
	}
	if len(wanted) == 0 || len(wanted) > 16 {
		return ErrPhysicalPlanningUnsupported
	}
	// DATABASE() is not used: the pool can have a different default schema.
	database := s.delivery.schema[1 : len(s.delivery.schema)-1]
	rows, err := tx.QueryContext(ctx, "SELECT COLUMN_NAME,SEQ_IN_INDEX,NON_UNIQUE,SUB_PART,EXPRESSION,INDEX_TYPE,IS_VISIBLE,COLLATION FROM information_schema.statistics WHERE TABLE_SCHEMA=? AND TABLE_NAME=? AND INDEX_NAME=? ORDER BY SEQ_IN_INDEX LIMIT 17", database, physical.Name, "record_index_"+physicalUUID(job.ID))
	if err != nil {
		return err
	}
	defer rows.Close()
	i := 0
	for rows.Next() {
		var column, expression, order sql.NullString
		var prefix sql.NullInt64
		var sequence, nonUnique int
		var kind, visible string
		if err := rows.Scan(&column, &sequence, &nonUnique, &prefix, &expression, &kind, &visible, &order); err != nil {
			return err
		}
		if i >= len(wanted) || !column.Valid || column.String != wanted[i] || sequence != i+1 || nonUnique != 1 || int(prefix.Int64) != prefixes[i] || prefix.Valid != (prefixes[i] != 0) || expression.Valid || kind != "BTREE" || visible != "YES" || order.String != "A" {
			return ErrPhysicalConflict
		}
		i++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if i != len(wanted) {
		return ErrPhysicalConflict
	}
	return nil
}
