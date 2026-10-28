package mysql

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var ErrModelUnavailable = errors.New("physical model is not published or has an active schema barrier")
var ErrPhysicalConflict = errors.New("physical model version, operation or mapping conflict")

type PhysicalTable struct {
	TableID uuid.UUID
	Name    string
	Version uuid.UUID
	Epoch   int64
}

type PhysicalField struct {
	FieldID         uuid.UUID
	Component, Name string
}

// PhysicalRegistry participates in the caller's transaction. It never performs
// DDL or authorizes a tenant. Registration is metadata only, not readiness.
// The caller must roll back the whole transaction on any method error.
type PhysicalRegistry struct{ metadataRepository }

func NewPhysicalRegistry(tx *sql.Tx, database string, tenant uuid.UUID, maxRows int) (PhysicalRegistry, error) {
	r, err := NewMetadataRepositories(tx, database, tenant, maxRows, 1<<20)
	if err != nil {
		return PhysicalRegistry{}, err
	}
	return PhysicalRegistry{r.Tables.metadataRepository}, nil
}

func (r PhysicalRegistry) RegisterTable(ctx context.Context, table uuid.UUID) error {
	if table == uuid.Nil {
		return errors.New("model table required")
	}
	var archived bool
	var tableName, tenantName string
	if err := r.tx.QueryRowContext(ctx, "SELECT archived,LEFT(name,64) FROM "+r.schema+".data_model_tables WHERE tenant_id=? AND id=? FOR UPDATE", mysqlstore.UUID(r.tenant), mysqlstore.UUID(table)).Scan(&archived, &tableName); err != nil {
		return err
	}
	if archived {
		return ErrModelUnavailable
	}
	// The tenant label is advisory; reading it does not acquire a shared tenant
	// mutation lock or serialize otherwise independent table registrations.
	if err := r.tx.QueryRowContext(ctx, "SELECT LEFT(name,64) FROM "+r.schema+".data_model_tenants WHERE id=?", mysqlstore.UUID(r.tenant)).Scan(&tenantName); err != nil {
		return err
	}
	// Duplicate registration preserves the original labels even after renames.
	_, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".data_model_physical_tables(tenant_id,table_id,tenant_label,table_label) VALUES (?,?,?,?) ON DUPLICATE KEY UPDATE table_id=table_id", mysqlstore.UUID(r.tenant), mysqlstore.UUID(table), physicalLabel(tenantName, 10, "tenant"), physicalLabel(tableName, 12, "table"))
	return err
}

// LockPublished must precede physical record access. Its shared registry lock
// remains held through the caller's data transaction. Schema preparation takes
// an exclusive lock on exactly this row, waiting for previous writers to finish.
// A nonzero expected version is mandatory; callers must refresh stale contracts.
func (r PhysicalRegistry) LockPublished(ctx context.Context, table, expected uuid.UUID) (PhysicalTable, error) {
	if table == uuid.Nil || expected == uuid.Nil {
		return PhysicalTable{}, errors.New("table and expected published version required")
	}
	var v PhysicalTable
	var version, operation mysqlstore.NullUUID
	var state string
	err := r.tx.QueryRowContext(ctx, "SELECT table_id,physical_name,published_version,epoch,write_state,active_operation FROM "+r.schema+".data_model_physical_tables WHERE tenant_id=? AND table_id=? FOR SHARE", mysqlstore.UUID(r.tenant), mysqlstore.UUID(table)).Scan((*mysqlstore.UUID)(&v.TableID), &v.Name, &version, &v.Epoch, &state, &operation)
	if err != nil {
		return PhysicalTable{}, err
	}
	if state != "open" || !version.Valid || operation.Valid {
		return PhysicalTable{}, ErrModelUnavailable
	}
	if uuid.UUID(version.UUID) != expected {
		return PhysicalTable{}, ErrPhysicalConflict
	}
	v.Version = expected
	return v, nil
}

// RegisterFields registers at most 1,000 model fields in one call. IP uses three
// components; scalar types use value. Existing mappings must be complete and
// agree with the model type. Archive does not discard mappings. The registry
// must remain closed until physical verification and owner publication finish.
func (r PhysicalRegistry) RegisterFields(ctx context.Context, table uuid.UUID, ids []uuid.UUID) error {
	if table == uuid.Nil || len(ids) == 0 || len(ids) > 1000 || len(ids) > r.maxRows {
		return errors.New("table and 1..1000 bounded field identities required")
	}
	ids = append([]uuid.UUID(nil), ids...)
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	for i, id := range ids {
		if id == uuid.Nil || (i > 0 && ids[i-1] == id) {
			return errors.New("distinct nonzero field identities required")
		}
	}
	if err := r.lock(ctx, "data_model_tables", table, "", uuid.Nil); err != nil {
		return err
	}
	var state string
	if err := r.tx.QueryRowContext(ctx, "SELECT write_state FROM "+r.schema+".data_model_physical_tables WHERE tenant_id=? AND table_id=? FOR UPDATE", mysqlstore.UUID(r.tenant), mysqlstore.UUID(table)).Scan(&state); err != nil {
		return err
	}
	if state != "closed" {
		return ErrModelUnavailable
	}
	args := []any{mysqlstore.UUID(r.tenant), mysqlstore.UUID(table)}
	for _, id := range ids {
		args = append(args, mysqlstore.UUID(id))
	}
	rows, err := r.tx.QueryContext(ctx, "SELECT id,data_type,archived,LEFT(name,64) FROM "+r.schema+".data_model_fields WHERE tenant_id=? AND table_id=? AND id IN ("+placeholders(len(ids))+") ORDER BY id FOR SHARE", args...)
	if err != nil {
		return err
	}
	want := make(map[uuid.UUID][]string, len(ids))
	labels := make(map[uuid.UUID]string, len(ids))
	for rows.Next() {
		var id mysqlstore.UUID
		var typ string
		var archived bool
		var fieldName string
		if err := rows.Scan(&id, &typ, &archived, &fieldName); err != nil {
			rows.Close()
			return err
		}
		if archived {
			rows.Close()
			return ErrModelUnavailable
		}
		components := []string{"value"}
		if typ == "ip_address" {
			components = []string{"address", "family", "prefix"}
		}
		want[uuid.UUID(id)] = components
		labels[uuid.UUID(id)] = physicalLabel(fieldName, 12, "field")
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil || closeErr != nil {
		return errors.Join(err, closeErr)
	}
	if len(want) != len(ids) {
		return sql.ErrNoRows
	}
	rows, err = r.tx.QueryContext(ctx, "SELECT field_id,component FROM "+r.schema+".data_model_physical_fields WHERE tenant_id=? AND table_id=? AND field_id IN ("+placeholders(len(ids))+") ORDER BY field_id,component", args...)
	if err != nil {
		return err
	}
	existing := make(map[uuid.UUID][]string)
	for rows.Next() {
		var id mysqlstore.UUID
		var component string
		if err := rows.Scan(&id, &component); err != nil {
			rows.Close()
			return err
		}
		existing[uuid.UUID(id)] = append(existing[uuid.UUID(id)], component)
	}
	err = rows.Err()
	closeErr = rows.Close()
	if err != nil || closeErr != nil {
		return errors.Join(err, closeErr)
	}
	var values []string
	var bindings []any
	flush := func() error {
		if len(values) == 0 {
			return nil
		}
		_, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".data_model_physical_fields(tenant_id,table_id,field_id,component,field_label) VALUES "+strings.Join(values, ","), bindings...)
		values = nil
		bindings = nil
		return err
	}
	for _, id := range ids {
		if found, ok := existing[id]; ok {
			if strings.Join(found, ",") != strings.Join(want[id], ",") {
				return ErrPhysicalConflict
			}
			continue
		}
		for _, component := range want[id] {
			values = append(values, "(?,?,?,?,?)")
			bindings = append(bindings, mysqlstore.UUID(r.tenant), mysqlstore.UUID(table), mysqlstore.UUID(id), component, labels[id])
			if len(values) == 1000 {
				if err := flush(); err != nil {
					return err
				}
			}
		}
	}
	return flush()
}

// Fields is one bounded ordered read, including archived mappings. Consumers
// resolve logical fields separately and must acquire LockPublished first for
// physical record access. No mapping existence check implies readiness.
func (r PhysicalRegistry) Fields(ctx context.Context, table uuid.UUID) ([]PhysicalField, error) {
	if table == uuid.Nil {
		return nil, errors.New("table required")
	}
	rows, err := r.tx.QueryContext(ctx, "SELECT p.field_id,p.component,p.physical_name,f.data_type FROM "+r.schema+".data_model_physical_fields p JOIN "+r.schema+".data_model_fields f ON f.tenant_id=p.tenant_id AND f.table_id=p.table_id AND f.id=p.field_id WHERE p.tenant_id=? AND p.table_id=? ORDER BY p.field_id,p.component LIMIT ?", mysqlstore.UUID(r.tenant), mysqlstore.UUID(table), r.maxRows+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]PhysicalField, 0)
	var current uuid.UUID
	var components []string
	want := ""
	for rows.Next() {
		if len(result) == r.maxRows {
			return nil, ErrSnapshotLimit
		}
		var v PhysicalField
		var typ string
		if err := rows.Scan((*mysqlstore.UUID)(&v.FieldID), &v.Component, &v.Name, &typ); err != nil {
			return nil, err
		}
		if v.FieldID != current {
			if current != uuid.Nil && strings.Join(components, ",") != want {
				return nil, ErrPhysicalConflict
			}
			current = v.FieldID
			components = nil
			want = "value"
			if typ == "ip_address" {
				want = "address,family,prefix"
			}
		}
		components = append(components, v.Component)
		result = append(result, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if current != uuid.Nil && strings.Join(components, ",") != want {
		return nil, ErrPhysicalConflict
	}
	return result, nil
}
