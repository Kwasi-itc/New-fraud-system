package mysqlruntime

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/reconcile"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
	"sort"
	"strings"
)

type localReconcileReport struct {
	reconcile.Report
	UnexpectedPhysicalTables []string `json:"unexpected_physical_tables"`
	Observation              string   `json:"observation"`
}
type reconcileTableState struct {
	tenant, id           uuid.UUID
	name, physical, gate string
	columns              map[string]bool
}
type reconcileGap struct {
	tenant, table uuid.UUID
	columns       []string
}

// Reconciliation is a bounded observation, not a readiness certificate. Metadata
// is snapshotted in bulk; information_schema can change concurrently with DDL.
// Repair delivery is committed after inspection, never while holding read locks.
func (p *LocalCreation) reconcileLocal(ctx context.Context, tenantScope uuid.UUID) (localReconcileReport, error) {
	result := localReconcileReport{Report: reconcile.Report{Healthy: true, Tenants: []reconcile.TenantReport{}}, UnexpectedPhysicalTables: []string{}, Observation: "schema names, columns and managed index coverage; concurrent DDL may change this observation; queued repairs require a worker"}
	tenantIndex := map[uuid.UUID]int{}
	tables := map[uuid.UUID]*reconcileTableState{}
	physical := map[string]*reconcileTableState{}
	actual := map[string]map[string]bool{}
	indexes := map[string]map[string][]string{}
	gaps := []reconcileGap{}
	// Global row/byte limits fail explicitly instead of reporting partial health.
	scope := func(column string) string {
		if tenantScope == uuid.Nil {
			return ""
		}
		return " AND " + column + "=?"
	}
	physicalScope := ""
	if tenantScope != uuid.Nil {
		physicalScope = " AND TABLE_NAME IN (SELECT physical_name FROM fraud.data_model_physical_tables WHERE tenant_id=?)"
	}
	remaining := 10000
	bytes := 0
	scan := func(tx *sql.Tx, query string, fn func(*sql.Rows) error) error {
		args := make([]any, strings.Count(query, "?"))
		for i := range args {
			args[i] = mysqlstore.UUID(tenantScope)
		}
		rows, e := tx.QueryContext(ctx, query+" LIMIT 10001", args...)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			remaining--
			if remaining < 0 {
				return store.ErrSnapshotLimit
			}
			if e = fn(rows); e != nil {
				return e
			}
			if bytes > localMaxBytes {
				return store.ErrSnapshotLimit
			}
		}
		return rows.Err()
	}
	err := mysqlstore.Snapshot(ctx, p.Target, func(tx *sql.Tx) error {
		if e := localAuthority(ctx, tx); e != nil {
			return e
		}
		e := scan(tx, "SELECT id,name,status FROM fraud.data_model_tenants WHERE TRUE"+scope("id")+" ORDER BY id", func(r *sql.Rows) error {
			var id mysqlstore.UUID
			var name, status string
			if e := r.Scan(&id, &name, &status); e != nil {
				return e
			}
			bytes += len(name) + len(status)
			tenantIndex[uuid.UUID(id)] = len(result.Tenants)
			result.Tenants = append(result.Tenants, reconcile.TenantReport{TenantID: uuid.UUID(id), TenantName: name, SchemaName: "fraud", SchemaExists: true, MissingTables: []string{}, UnexpectedTables: []string{}, TableReports: []reconcile.TableReport{}, SchemaHealthy: status == "active"})
			return nil
		})
		if e != nil {
			return e
		}
		e = scan(tx, "SELECT t.tenant_id,t.id,t.name,COALESCE(p.physical_name,''),COALESCE(p.write_state,'closed') FROM fraud.data_model_tables t LEFT JOIN fraud.data_model_physical_tables p ON p.tenant_id=t.tenant_id AND p.table_id=t.id WHERE NOT t.archived"+scope("t.tenant_id")+" ORDER BY t.tenant_id,t.id", func(r *sql.Rows) error {
			v := &reconcileTableState{columns: map[string]bool{"id": true, "valid_from": true, "valid_until": true, "record_object_key_id": true}}
			if e := r.Scan((*mysqlstore.UUID)(&v.tenant), (*mysqlstore.UUID)(&v.id), &v.name, &v.physical, &v.gate); e != nil {
				return e
			}
			bytes += len(v.name) + len(v.physical)
			tables[v.id] = v
			if v.physical != "" {
				physical[v.physical] = v
			}
			return nil
		})
		if e != nil {
			return e
		}
		e = scan(tx, "SELECT f.table_id,f.id,f.name,f.data_type,f.is_unique,COALESCE(p.physical_name,'') FROM fraud.data_model_fields f JOIN fraud.data_model_tables t ON t.tenant_id=f.tenant_id AND t.id=f.table_id LEFT JOIN fraud.data_model_physical_fields p ON p.tenant_id=f.tenant_id AND p.table_id=f.table_id AND p.field_id=f.id WHERE NOT t.archived AND NOT f.archived"+scope("t.tenant_id")+" ORDER BY f.table_id,f.id,p.component", func(r *sql.Rows) error {
			var table, id mysqlstore.UUID
			var name, typ, column string
			var unique bool
			if e := r.Scan(&table, &id, &name, &typ, &unique, &column); e != nil {
				return e
			}
			v := tables[uuid.UUID(table)]
			if v == nil {
				return store.ErrPhysicalConflict
			}
			if column == "" {
				v.columns["unmapped:"+name] = true
			} else {
				v.columns[column] = true
			}
			if typ == "string" && unique && name != "object_id" {
				v.columns["record_text_key_"+strings.ReplaceAll(uuid.UUID(id).String(), "-", "")] = true
			}
			bytes += len(name) + len(column)
			return nil
		})
		if e != nil {
			return e
		}
		e = scan(tx, "SELECT TABLE_NAME,COLUMN_NAME FROM information_schema.columns WHERE TABLE_SCHEMA='fraud' AND LEFT(TABLE_NAME,7)='tenant_'"+physicalScope+" ORDER BY TABLE_NAME,ORDINAL_POSITION", func(r *sql.Rows) error {
			var t, c string
			if e := r.Scan(&t, &c); e != nil {
				return e
			}
			if actual[t] == nil {
				actual[t] = map[string]bool{}
			}
			actual[t][c] = true
			bytes += len(t) + len(c)
			return nil
		})
		if e != nil {
			return e
		}
		e = scan(tx, "SELECT TABLE_NAME,INDEX_NAME,COLUMN_NAME,SUB_PART FROM information_schema.statistics WHERE TABLE_SCHEMA='fraud' AND LEFT(TABLE_NAME,7)='tenant_' AND EXPRESSION IS NULL AND IS_VISIBLE='YES' AND INDEX_TYPE='BTREE'"+physicalScope+" ORDER BY TABLE_NAME,INDEX_NAME,SEQ_IN_INDEX", func(r *sql.Rows) error {
			var t, i, c string
			var prefix sql.NullInt64
			if e := r.Scan(&t, &i, &c, &prefix); e != nil {
				return e
			}
			if indexes[t] == nil {
				indexes[t] = map[string][]string{}
			}
			if prefix.Valid {
				c += fmt.Sprintf("(%d)", prefix.Int64)
			}
			indexes[t][i] = append(indexes[t][i], c)
			bytes += len(t) + len(i) + len(c)
			return nil
		})
		if e != nil {
			return e
		}
		// Caption and navigation expectations are joined to their physical columns.
		return scan(tx, "SELECT t.tenant_id,t.id,f.name,COALESCE(p.physical_name,''),'','',f.data_type,'' FROM fraud.data_model_tables t JOIN fraud.data_model_fields f ON f.tenant_id=t.tenant_id AND f.table_id=t.id AND f.name=t.caption_field LEFT JOIN fraud.data_model_physical_fields p ON p.tenant_id=f.tenant_id AND p.table_id=f.table_id AND p.field_id=f.id AND p.component='value' WHERE NOT t.archived AND t.caption_field<>''"+scope("t.tenant_id")+" UNION ALL SELECT n.tenant_id,n.target_table_id,f.name,COALESCE(p.physical_name,''),o.name,COALESCE(q.physical_name,''),f.data_type,o.data_type FROM fraud.data_model_navigation_options n JOIN fraud.data_model_fields f ON f.tenant_id=n.tenant_id AND f.id=n.filter_field_id JOIN fraud.data_model_fields o ON o.tenant_id=n.tenant_id AND o.id=n.ordering_field_id LEFT JOIN fraud.data_model_physical_fields p ON p.tenant_id=f.tenant_id AND p.table_id=f.table_id AND p.field_id=f.id AND p.component='value' LEFT JOIN fraud.data_model_physical_fields q ON q.tenant_id=o.tenant_id AND q.table_id=o.table_id AND q.field_id=o.id AND q.component='value' WHERE TRUE"+scope("n.tenant_id"), func(r *sql.Rows) error {
			var tenant, table mysqlstore.UUID
			var name, column, second, secondColumn string
			var firstType, secondType string
			if e := r.Scan(&tenant, &table, &name, &column, &second, &secondColumn, &firstType, &secondType); e != nil {
				return e
			}
			t := tables[uuid.UUID(table)]
			if t == nil {
				return store.ErrPhysicalConflict
			}
			names := []string{name}
			if firstType == "string" && column != "" {
				column += "(191)"
			}
			if secondType == "string" && secondColumn != "" {
				secondColumn += "(191)"
			}
			cols := []string{column}
			if second != "" {
				names = append(names, second)
				cols = append(cols, secondColumn)
			}
			covered := false
			for _, index := range indexes[t.physical] {
				if len(index) >= len(cols) {
					match := true
					for i, c := range cols {
						if c == "" || index[i] != c {
							match = false
						}
					}
					if match {
						covered = true
						break
					}
				}
			}
			if !covered {
				gaps = append(gaps, reconcileGap{uuid.UUID(tenant), uuid.UUID(table), names})
			}
			return nil
		})
	})
	if err != nil {
		return result, err
	}
	if tenantScope != uuid.Nil && len(result.Tenants) == 0 {
		return result, sql.ErrNoRows
	}
	if len(gaps) > 100 {
		return result, store.ErrSnapshotLimit
	}
	for name := range actual {
		if physical[name] == nil {
			result.UnexpectedPhysicalTables = append(result.UnexpectedPhysicalTables, name)
			result.Healthy = false
		}
	}
	sort.Strings(result.UnexpectedPhysicalTables)
	ids := make([]uuid.UUID, 0, len(tables))
	for id := range tables {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	for _, id := range ids {
		t := tables[id]
		ti, ok := tenantIndex[t.tenant]
		if !ok {
			return result, store.ErrPhysicalConflict
		}
		tenant := &result.Tenants[ti]
		tr := reconcile.TableReport{TableName: t.name, PhysicalExists: actual[t.physical] != nil, MissingColumns: []string{}, UnexpectedColumns: []string{}, MissingManagedIndexes: []reconcile.ManagedIndexGap{}, RepairJobIDs: []uuid.UUID{}, Healthy: t.gate == "open"}
		for c := range t.columns {
			if !actual[t.physical][c] {
				tr.MissingColumns = append(tr.MissingColumns, c)
			}
		}
		for c := range actual[t.physical] {
			if !t.columns[c] {
				tr.UnexpectedColumns = append(tr.UnexpectedColumns, c)
			}
		}
		if !tr.PhysicalExists {
			tenant.MissingTables = append(tenant.MissingTables, t.name)
		}
		seen := map[string]bool{}
		for _, gap := range gaps {
			if gap.table != id {
				continue
			}
			key := strings.Join(gap.columns, "\x00")
			if seen[key] {
				continue
			}
			seen[key] = true
			typ := "search"
			if len(gap.columns) > 1 {
				typ = "navigation"
			}
			tr.MissingManagedIndexes = append(tr.MissingManagedIndexes, reconcile.ManagedIndexGap{IndexType: typ, Columns: gap.columns})
			if tr.PhysicalExists && t.gate == "open" && len(tr.MissingColumns) == 0 {
				queue, e := localIndexQueue(p.Target)
				if e != nil {
					return result, e
				}
				app, e := store.NewIndexJobApplication(p.Target, "fraud", t.tenant, localMaxRows, localMaxBytes, localTenantAuthority, localTenantAuthority, queue, localIDs{}, localClock{})
				if e != nil {
					return result, e
				}
				job, scheduled, e := app.EnsureRepair(ctx, id, gap.columns)
				if e != nil {
					return result, e
				}
				tr.RepairJobIDs = append(tr.RepairJobIDs, job)
				if scheduled {
					tenant.RepairJobsScheduled++
					result.RepairJobsScheduled++
				}
			}
		}
		sort.Strings(tr.MissingColumns)
		sort.Strings(tr.UnexpectedColumns)
		tr.Healthy = tr.Healthy && tr.PhysicalExists && len(tr.MissingColumns) == 0 && len(tr.UnexpectedColumns) == 0 && len(tr.MissingManagedIndexes) == 0
		if !tr.Healthy {
			tenant.SchemaHealthy = false
		}
		tenant.TableReports = append(tenant.TableReports, tr)
	}
	for _, t := range result.Tenants {
		if !t.SchemaHealthy {
			result.Healthy = false
		}
	}
	encoded, e := json.Marshal(result)
	if e != nil {
		return result, e
	}
	if len(encoded) > localMaxBytes {
		return result, store.ErrSnapshotLimit
	}
	return result, nil
}
