package mysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var ErrTableDeletionDependency = errors.New("remove table links, pivots, external display references, navigation and pending/running index jobs before deleting")

// TableRetirement is separate from the ordinary publication protocol: deletion
// must never reopen the write gate. Immutable audit entries retain its intent and
// completion across implicit DDL commits. Keep these entries with owner receipts.
type TableRetirement struct {
	db               *sql.DB
	database, schema string
	tenant           uuid.UUID
	authorize        MutationAuthorizer
}

func NewTableRetirement(db *sql.DB, database string, tenant uuid.UUID, authorize MutationAuthorizer) (*TableRetirement, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if db == nil || tenant == uuid.Nil || authorize == nil {
		return nil, ErrPhysicalConflict
	}
	return &TableRetirement{db, database, schema, tenant, authorize}, nil
}

type tableRetirementIntent struct {
	Table        uuid.UUID `json:"table_id"`
	Operation    uuid.UUID `json:"operation_id"`
	Name         string    `json:"physical_name"`
	OriginalName string    `json:"original_name"`
}

func retirementReceipt(table, op uuid.UUID, phase string) uuid.UUID {
	return uuid.NewSHA1(table, []byte("table-retirement/v1/"+op.String()+"/"+phase))
}

func (s *TableRetirement) read(ctx context.Context, tx *sql.Tx, table, op uuid.UUID, phase string) (tableRetirementIntent, error) {
	var raw []byte
	var result tableRetirementIntent
	err := tx.QueryRowContext(ctx, "SELECT details FROM "+s.schema+".data_model_schema_change_log WHERE tenant_id=? AND id=? AND operation='delete_table' AND OCTET_LENGTH(details)<=4096", mysqlstore.UUID(s.tenant), mysqlstore.UUID(retirementReceipt(table, op, phase))).Scan(&raw)
	if err != nil {
		return result, err
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return result, err
	}
	if result.Table != table || result.Operation != op {
		return result, ErrPhysicalConflict
	}
	return result, nil
}
func (s *TableRetirement) write(ctx context.Context, tx *sql.Tx, v tableRetirementIntent, phase string) error {
	r, err := NewMetadataRepositories(tx, s.database, s.tenant, 1000, 4<<20)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return r.History.Create(ctx, datamodel.SchemaChange{ID: retirementReceipt(v.Table, v.Operation, phase), TenantID: s.tenant, Operation: "delete_table", ResourceType: "table", ResourceID: v.Table, Status: phase, Details: raw, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)})
}

// check uses table-specific indexes and bounded conflict projections. Its caller
// holds the exact table row, which all mounted relationship writers also lock.
func (s *TableRetirement) check(ctx context.Context, tx *sql.Tx, table uuid.UUID) (datamodel.DeleteReport, error) {
	report := datamodel.NewDeleteReport()
	for _, spec := range []struct {
		object, predicate string
		ids               *[]uuid.UUID
	}{
		{"data_model_links", "parent_table_id=?", &report.Conflicts.Links},
		{"data_model_links", "child_table_id=?", &report.Conflicts.Links},
		{"data_model_pivots", "base_table_id=?", &report.Conflicts.Pivots},
	} {
		rows, err := tx.QueryContext(ctx, "SELECT id FROM "+s.schema+"."+spec.object+" WHERE tenant_id=? AND "+spec.predicate+" LIMIT 1001 FOR SHARE", mysqlstore.UUID(s.tenant), mysqlstore.UUID(table))
		if err != nil {
			return report, err
		}
		for rows.Next() {
			var id mysqlstore.UUID
			if err = rows.Scan(&id); err != nil {
				break
			}
			*spec.ids = append(*spec.ids, uuid.UUID(id))
		}
		err = errors.Join(err, rows.Err(), rows.Close())
		if err != nil {
			return report, err
		}
		if len(*spec.ids) > 1000 {
			return report, ErrSnapshotLimit
		}
	}
	if len(report.Conflicts.Links) > 0 || len(report.Conflicts.Pivots) > 0 {
		return report, ErrTableDeletionDependency
	}
	for _, spec := range []struct{ object, predicate string }{
		{"data_model_navigation_options", "source_table_id=?"},
		{"data_model_navigation_options", "target_table_id=?"},
		{"data_model_display_table_references", "parent_table_id=? AND table_id<>parent_table_id"},
		{"data_model_index_jobs", "table_id=? AND status IN ('pending','running')"},
	} {
		var found int
		err := tx.QueryRowContext(ctx, "SELECT 1 FROM "+s.schema+"."+spec.object+" WHERE tenant_id=? AND "+spec.predicate+" LIMIT 1 FOR SHARE", mysqlstore.UUID(s.tenant), mysqlstore.UUID(table)).Scan(&found)
		if err == nil {
			return report, ErrTableDeletionDependency
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return report, err
		}
	}
	return report, nil
}

func (s *TableRetirement) Delete(ctx context.Context, table, op uuid.UUID, dry bool) (datamodel.DeleteReport, error) {
	report := datamodel.NewDeleteReport()
	if table == uuid.Nil || op == uuid.Nil {
		return report, ErrPhysicalConflict
	}
	sum := sha256.Sum256([]byte(s.database + ":" + s.tenant.String() + ":" + table.String()))
	err := mysqlstore.WithDDLLock(ctx, s.db, fmt.Sprintf("data_model_ddl_%x", sum[:24]), func(conn *sql.Conn) error {
		var intent tableRetirementIntent
		completed := false
		err := mysqlstore.Mutation(ctx, s.db, func(tx *sql.Tx) error {
			if err := s.authorize(ctx, tx, s.tenant); err != nil {
				return err
			}
			var name, status string
			var archived bool
			if err := tx.QueryRowContext(ctx, "SELECT name,archived FROM "+s.schema+".data_model_tables WHERE tenant_id=? AND id=? FOR UPDATE", mysqlstore.UUID(s.tenant), mysqlstore.UUID(table)).Scan(&name, &archived); err != nil {
				return err
			}
			if err := tx.QueryRowContext(ctx, "SELECT status FROM "+s.schema+".data_model_tenants WHERE id=?", mysqlstore.UUID(s.tenant)).Scan(&status); err != nil {
				return err
			}
			if status != "active" {
				return ErrModelUnavailable
			}
			prior, e := s.read(ctx, tx, table, op, "completed")
			if e == nil {
				intent = prior
				completed = true
				report.Performed = !dry
				return nil
			}
			if !errors.Is(e, sql.ErrNoRows) {
				return e
			}
			if archived {
				return sql.ErrNoRows
			}
			report, e = s.check(ctx, tx, table)
			if e != nil || dry {
				return e
			}
			var physical, state string
			var epoch int64
			var active, version mysqlstore.NullUUID
			if e = tx.QueryRowContext(ctx, "SELECT physical_name,write_state,active_operation,published_version,epoch FROM "+s.schema+".data_model_physical_tables WHERE tenant_id=? AND table_id=? FOR UPDATE", mysqlstore.UUID(s.tenant), mysqlstore.UUID(table)).Scan(&physical, &state, &active, &version, &epoch); e != nil {
				return e
			}
			if !strings.HasSuffix(physical, "_"+strings.ReplaceAll(table.String(), "-", "")) || !strings.HasPrefix(physical, "tenant_") {
				return ErrPhysicalConflict
			}
			intent, e = s.read(ctx, tx, table, op, "started")
			if e == nil {
				if intent.Name != physical || state != "closed" || !active.Valid || uuid.UUID(active.UUID) != op {
					return ErrPhysicalConflict
				}
				return nil
			}
			if !errors.Is(e, sql.ErrNoRows) {
				return e
			}
			if state != "open" || active.Valid || !version.Valid {
				return ErrModelUnavailable
			}
			intent = tableRetirementIntent{table, op, physical, name}
			if e = s.write(ctx, tx, intent, "started"); e != nil {
				return e
			}
			intents, e := NewDDLIntentStore(s.db, s.database, s.tenant, 4096)
			if e != nil {
				return e
			}
			shape, e := json.Marshal(struct {
				Kind   string                `json:"kind"`
				Intent tableRetirementIntent `json:"intent"`
			}{"table-retirement/v1", intent})
			if e != nil {
				return e
			}
			expected := uuid.UUID(version.UUID)
			_, e = intents.PrepareTx(ctx, tx, DDLIntent{TableID: table, OperationID: op, RequestHash: sha256.Sum256(shape), ActorKind: "api", ActorID: "local-table-retirement", ExpectedEpoch: epoch, ExpectedVersion: &expected, Shape: shape})
			return e
		})
		if err != nil || dry || completed {
			return err
		}
		// Same pinned DDL connection/lock for DROP and verification. No row locks or
		// network calls span DDL. A lost response leaves the durable gate closed.
		name, err := mysqlstore.Identifier(intent.Name)
		if err != nil {
			return err
		}
		if _, err = conn.ExecContext(ctx, "SET SESSION lock_wait_timeout=5"); err != nil {
			return err
		}
		if _, err = conn.ExecContext(ctx, "DROP TABLE IF EXISTS "+s.schema+"."+name); err != nil {
			return err
		}
		var count int
		if err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE TABLE_SCHEMA=? AND TABLE_NAME=?", s.database, intent.Name).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return ErrPhysicalConflict
		}
		err = mysqlstore.Mutation(ctx, s.db, func(tx *sql.Tx) error {
			if e := s.authorize(ctx, tx, s.tenant); e != nil {
				return e
			}
			var got mysqlstore.UUID
			if e := tx.QueryRowContext(ctx, "SELECT id FROM "+s.schema+".data_model_tables WHERE tenant_id=? AND id=? AND NOT archived FOR UPDATE", mysqlstore.UUID(s.tenant), mysqlstore.UUID(table)).Scan(&got); e != nil {
				return e
			}
			var e error
			report, e = s.check(ctx, tx, table)
			if e != nil {
				return e
			}
			var state string
			var active mysqlstore.NullUUID
			if e = tx.QueryRowContext(ctx, "SELECT write_state,active_operation FROM "+s.schema+".data_model_physical_tables WHERE tenant_id=? AND table_id=? FOR UPDATE", mysqlstore.UUID(s.tenant), mysqlstore.UUID(table)).Scan(&state, &active); e != nil {
				return e
			}
			if state != "closed" || !active.Valid || uuid.UUID(active.UUID) != op {
				return ErrPhysicalConflict
			}
			// Retain immutable creation/DDL/publication lineage under a non-user name,
			// while releasing the original logical name for a new explicit create key.
			for _, object := range []string{"data_model_display_field_references", "data_model_display_link_references", "data_model_display_table_references", "data_model_table_options", "data_model_ingestion_contracts"} {
				if _, e = tx.ExecContext(ctx, "DELETE FROM "+s.schema+"."+object+" WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(s.tenant), mysqlstore.UUID(table)); e != nil {
					return e
				}
			}
			if _, e = tx.ExecContext(ctx, "UPDATE "+s.schema+".data_model_tables SET name=?,archived=1,caption_field='',updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=?", "!deleted:"+table.String(), mysqlstore.UUID(s.tenant), mysqlstore.UUID(table)); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, "UPDATE "+s.schema+".data_model_fields SET archived=1 WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(s.tenant), mysqlstore.UUID(table)); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, "UPDATE "+s.schema+".data_model_physical_tables SET write_state='retired',active_operation=NULL WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(s.tenant), mysqlstore.UUID(table)); e != nil {
				return e
			}
			r, e := NewMetadataRepositories(tx, s.database, s.tenant, 1000, 4<<20)
			if e != nil {
				return e
			}
			if e = r.Revisions.Create(ctx, datamodel.TenantSchemaMigration{ID: uuid.New(), TenantID: s.tenant, Version: "delete_table:table", AppliedAt: time.Now().UTC().Truncate(time.Microsecond)}); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, "UPDATE "+s.schema+".data_model_schema_change_intents SET state='published',updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND table_id=? AND operation_id=?", mysqlstore.UUID(s.tenant), mysqlstore.UUID(table), mysqlstore.UUID(op)); e != nil {
				return e
			}
			return s.write(ctx, tx, intent, "completed")
		})
		if err == nil {
			report.Performed = true
		}
		return err
	})
	return report, err
}
