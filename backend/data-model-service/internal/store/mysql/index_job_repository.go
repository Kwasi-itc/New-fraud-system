package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// IndexJobRepository is caller-transaction metadata persistence, not delivery or
// worker fencing. Workers must use IndexJobExecutionStore for fenced outcomes
// with atomic history. The owner must roll back its transaction on any error.
type IndexJobRepository struct{ metadataRepository }

func jobTime(v *time.Time) mysqlstore.NullDateTime {
	if v == nil {
		return mysqlstore.NullDateTime{}
	}
	return mysqlstore.NullDateTime{Valid: true, DateTime: mysqlstore.DateTime{Time: *v}}
}

func (r IndexJobRepository) Create(ctx context.Context, v datamodel.IndexJob) error {
	values := []string{v.TableName, string(v.IndexType), string(v.Status), v.RequestedByOperation, v.DedupeKey}
	if v.ErrorMessage != nil {
		values = append(values, *v.ErrorMessage)
	}
	// Bound raw input before JSON encoding; check escaped JSON separately below.
	if len(v.Columns) > r.maxRows {
		return ErrSnapshotLimit
	}
	values = append(values, v.Columns...)
	if err := r.validate(v.TenantID, v.ID, values...); err != nil {
		return err
	}
	if v.Columns == nil {
		return errors.New("index job columns must be an array")
	}
	columns, err := json.Marshal(v.Columns)
	if err != nil {
		return err
	}
	values = values[:len(values)-len(v.Columns)]
	values = append(values, string(columns))
	if err := r.validate(v.TenantID, v.ID, values...); err != nil {
		return err
	}
	if v.AttemptCount < 0 || v.AttemptCount > 2147483647 {
		return errors.New("index job attempt count outside signed INT range")
	}
	table := mysqlstore.NullUUID{}
	if v.TableID != nil {
		if *v.TableID == uuid.Nil {
			return errors.New("nonzero job table required")
		}
		table = mysqlstore.NullUUID{Valid: true, UUID: mysqlstore.UUID(*v.TableID)}
	}
	_, err = r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".data_model_index_jobs(id,tenant_id,table_id,table_name,index_type,columns_json,status,requested_by_operation,error_message,attempt_count,requested_at,started_at,completed_at,scheduled_at,dedupe_key) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", mysqlstore.UUID(v.ID), mysqlstore.UUID(v.TenantID), table, v.TableName, v.IndexType, mysqlstore.JSON(columns), v.Status, v.RequestedByOperation, v.ErrorMessage, v.AttemptCount, mysqlstore.DateTime{Time: v.RequestedAt}, jobTime(v.StartedAt), jobTime(v.CompletedAt), jobTime(v.ScheduledAt), v.DedupeKey)
	return err
}

func (r IndexJobRepository) selectSQL() string {
	return "SELECT id,tenant_id,table_id," + r.projection("table_name", "index_type", "CAST(columns_json AS CHAR CHARACTER SET utf8mb4)", "status", "requested_by_operation", "COALESCE(error_message,'')", "dedupe_key") + ",error_message IS NOT NULL,attempt_count,requested_at,started_at,completed_at,scheduled_at FROM " + r.schema + ".data_model_index_jobs WHERE tenant_id=?"
}

func (r IndexJobRepository) scan(row metadataScanner) (datamodel.IndexJob, int64, error) {
	var v datamodel.IndexJob
	var size int64
	var table mysqlstore.NullUUID
	var requested mysqlstore.DateTime
	var started, completed, scheduled mysqlstore.NullDateTime
	var columns []byte
	var message string
	var hasMessage bool
	err := row.Scan((*mysqlstore.UUID)(&v.ID), (*mysqlstore.UUID)(&v.TenantID), &table, &size, &v.TableName, &v.IndexType, &columns, &v.Status, &v.RequestedByOperation, &message, &v.DedupeKey, &hasMessage, &v.AttemptCount, &requested, &started, &completed, &scheduled)
	if err != nil {
		return datamodel.IndexJob{}, 0, err
	}
	if size > int64(r.maxBytes) {
		return datamodel.IndexJob{}, 0, ErrSnapshotLimit
	}
	var names []*string
	if err = json.Unmarshal(columns, &names); err != nil {
		return datamodel.IndexJob{}, 0, err
	}
	if names == nil {
		return datamodel.IndexJob{}, 0, errors.New("index job columns must be an array")
	}
	if len(names) > r.maxRows {
		return datamodel.IndexJob{}, 0, ErrSnapshotLimit
	}
	v.Columns = make([]string, len(names))
	for i, name := range names {
		if name == nil {
			return datamodel.IndexJob{}, 0, errors.New("index job column cannot be null")
		}
		v.Columns[i] = *name
	}
	v.RequestedAt = requested.Time
	if table.Valid {
		id := uuid.UUID(table.UUID)
		v.TableID = &id
	}
	if started.Valid {
		v.StartedAt = &started.DateTime.Time
	}
	if completed.Valid {
		v.CompletedAt = &completed.DateTime.Time
	}
	if scheduled.Valid {
		v.ScheduledAt = &scheduled.DateTime.Time
	}
	if hasMessage {
		v.ErrorMessage = &message
	}
	return v, size, nil
}

func (r IndexJobRepository) GetByID(ctx context.Context, id uuid.UUID) (datamodel.IndexJob, error) {
	return metadataOne(r.metadataRepository, r.tx.QueryRowContext(ctx, r.selectSQL()+" AND id=?", mysqlstore.UUID(r.tenant), mysqlstore.UUID(id)), r.scan)
}
func (r IndexJobRepository) ListByTenant(ctx context.Context, id uuid.UUID) ([]datamodel.IndexJob, error) {
	if id != r.tenant {
		return nil, errors.New("invalid job tenant scope")
	}
	return metadataList(ctx, r.metadataRepository, r.selectSQL()+" ORDER BY requested_at DESC,id DESC", []any{mysqlstore.UUID(r.tenant)}, r.scan)
}
func (r IndexJobRepository) GetByDedupeKey(ctx context.Context, key string) (*datamodel.IndexJob, error) {
	return r.byKey(ctx, key, false)
}
func (r IndexJobRepository) GetByDedupeKeyForUpdate(ctx context.Context, key string) (*datamodel.IndexJob, error) {
	return r.byKey(ctx, key, true)
}
func (r IndexJobRepository) byKey(ctx context.Context, key string, lock bool) (*datamodel.IndexJob, error) {
	if err := r.validate(r.tenant, r.tenant, key); err != nil {
		return nil, err
	}
	if key == "" {
		return nil, nil
	}
	// A nonunique generated digest narrows candidates; full binary equality
	// determines identity. This single-table read needs no private dictionary
	// access and remains a current read with FOR UPDATE under REPEATABLE READ.
	// Exact uniqueness is still enforced independently by the dictionary guard.
	query := r.selectSQL() + " AND dedupe_lookup_hash=UNHEX(SHA2(CAST(? AS BINARY),256)) AND CAST(dedupe_key AS BINARY)=CAST(? AS BINARY)"
	if lock {
		query += " FOR UPDATE"
	}
	v, err := metadataOne(r.metadataRepository, r.tx.QueryRowContext(ctx, query, mysqlstore.UUID(r.tenant), key, key), r.scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (r IndexJobRepository) update(ctx context.Context, id uuid.UUID, set string, args ...any) error {
	if err := r.lock(ctx, "data_model_index_jobs", id, "", uuid.Nil); err != nil {
		return err
	}
	args = append(args, mysqlstore.UUID(r.tenant), mysqlstore.UUID(id))
	_, err := r.tx.ExecContext(ctx, "UPDATE "+r.schema+".data_model_index_jobs SET "+set+" WHERE tenant_id=? AND id=?", args...)
	return err
}
func (r IndexJobRepository) StartAttempt(ctx context.Context, id uuid.UUID, at time.Time) (datamodel.IndexJob, error) {
	if err := r.lock(ctx, "data_model_index_jobs", id, "", uuid.Nil); err != nil {
		return datamodel.IndexJob{}, err
	}
	v, err := r.GetByID(ctx, id)
	if err != nil {
		return datamodel.IndexJob{}, err
	}
	if v.AttemptCount >= 2147483647 {
		return datamodel.IndexJob{}, errors.New("index job attempt count exhausted")
	}
	_, err = r.tx.ExecContext(ctx, "UPDATE "+r.schema+".data_model_index_jobs SET status='running',attempt_count=attempt_count+1,started_at=?,error_message=NULL,completed_at=NULL WHERE tenant_id=? AND id=?", mysqlstore.DateTime{Time: at}, mysqlstore.UUID(r.tenant), mysqlstore.UUID(id))
	if err != nil {
		return datamodel.IndexJob{}, err
	}
	return r.GetByID(ctx, id)
}
func (r IndexJobRepository) MarkApplied(ctx context.Context, id uuid.UUID, at time.Time) error {
	return r.update(ctx, id, "status='applied',completed_at=?,error_message=NULL", mysqlstore.DateTime{Time: at})
}
func (r IndexJobRepository) MarkFailed(ctx context.Context, id uuid.UUID, message string, at time.Time) error {
	if err := r.validate(r.tenant, id, message); err != nil {
		return err
	}
	return r.update(ctx, id, "status='failed',error_message=?,completed_at=?", message, mysqlstore.DateTime{Time: at})
}
func (r IndexJobRepository) MarkPendingRetry(ctx context.Context, id uuid.UUID, message string) error {
	if err := r.validate(r.tenant, id, message); err != nil {
		return err
	}
	return r.update(ctx, id, "status='pending',error_message=?,completed_at=NULL", message)
}
func (r IndexJobRepository) Retry(ctx context.Context, id uuid.UUID, at time.Time) error {
	return r.update(ctx, id, "status='pending',error_message=NULL,started_at=NULL,completed_at=NULL,scheduled_at=?", mysqlstore.DateTime{Time: at})
}
