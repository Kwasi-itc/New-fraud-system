package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// IndexJobExecutionStore implements metadata attempts/outcomes only. No queue,
// lease, DDL execution or production migration is implied by this staged store.
type IndexJobExecutionStore struct {
	db       *sql.DB
	schema   string
	maxBytes int
}

func NewIndexJobExecutionStore(db *sql.DB, database string, maxBytes int) (*IndexJobExecutionStore, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if db == nil || maxBytes < 1 || maxBytes > 64<<20 {
		return nil, errors.New("database and byte limit in 1..67108864 required")
	}
	return &IndexJobExecutionStore{db, schema, maxBytes}, nil
}

func (s *IndexJobExecutionStore) StartAttempt(ctx context.Context, id uuid.UUID, at time.Time) (datamodel.IndexJob, error) {
	if id == uuid.Nil || at.IsZero() {
		return datamodel.IndexJob{}, errors.New("job identity and start time required")
	}
	if _, err := (mysqlstore.DateTime{Time: at}).Value(); err != nil {
		return datamodel.IndexJob{}, err
	}
	var job datamodel.IndexJob
	err := mysqlstore.Mutation(ctx, s.db, func(tx *sql.Tx) error {
		// The lock makes the size preflight and subsequent read one stable row.
		var size int64
		if err := tx.QueryRowContext(ctx, "SELECT OCTET_LENGTH(table_name)+OCTET_LENGTH(index_type)+OCTET_LENGTH(columns_json)+OCTET_LENGTH(status)+OCTET_LENGTH(requested_by_operation)+COALESCE(OCTET_LENGTH(error_message),0)+OCTET_LENGTH(dedupe_key) FROM "+s.schema+".data_model_index_jobs WHERE id=? FOR UPDATE", mysqlstore.UUID(id)).Scan(&size); err != nil {
			return err
		}
		if size > int64(s.maxBytes) {
			return ErrSnapshotLimit
		}
		var err error
		job, err = s.readJob(ctx, tx, id)
		if err != nil {
			return err
		}
		switch job.Status {
		case datamodel.IndexJobStatusApplied, datamodel.IndexJobStatusFailed, datamodel.IndexJobStatusCancelled:
			return nil
		case datamodel.IndexJobStatusPending, datamodel.IndexJobStatusRunning:
		default:
			return errors.New("unknown index job state")
		}
		if job.AttemptCount < 0 || job.AttemptCount >= 2147483647 {
			return errors.New("index job attempt count outside signed INT range")
		}
		result, err := tx.ExecContext(ctx, "UPDATE "+s.schema+".data_model_index_jobs SET status='running',attempt_count=attempt_count+1,started_at=?,error_message=NULL,completed_at=NULL WHERE tenant_id=? AND id=? AND attempt_count=?", mysqlstore.DateTime{Time: at}, mysqlstore.UUID(job.TenantID), mysqlstore.UUID(id), job.AttemptCount)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return ports.ErrStaleIndexJobAttempt
		}
		job.Status = datamodel.IndexJobStatusRunning
		job.AttemptCount++
		job.StartedAt = &at
		job.CompletedAt, job.ErrorMessage = nil, nil
		return nil
	})
	if err != nil {
		return datamodel.IndexJob{}, err
	}
	return job, nil
}

func (s *IndexJobExecutionStore) CompleteAttempt(ctx context.Context, o ports.IndexJobOutcome) error {
	if err := o.Validate(); err != nil {
		return err
	}
	if len(o.Change.Details)+len(o.ErrorMessage) > s.maxBytes {
		return ErrSnapshotLimit
	}
	return mysqlstore.Mutation(ctx, s.db, func(tx *sql.Tx) error {
		return completeIndexAttemptTx(ctx, tx, s.schema, o)
	})
}

// completeIndexAttemptTx is shared by the standalone attempt store and leased
// delivery integration. The caller supplies its already-owned transaction.
func completeIndexAttemptTx(ctx context.Context, tx *sql.Tx, schema string, o ports.IndexJobOutcome) error {
	completed := mysqlstore.NullDateTime{}
	if o.Change.Status != string(datamodel.IndexJobStatusPending) {
		completed = mysqlstore.NullDateTime{Valid: true, DateTime: mysqlstore.DateTime{Time: o.Change.CreatedAt}}
	}
	message := sql.NullString{String: o.ErrorMessage, Valid: o.ErrorMessage != ""}
	result, err := tx.ExecContext(ctx, "UPDATE "+schema+".data_model_index_jobs SET status=?,error_message=?,completed_at=? WHERE tenant_id=? AND id=? AND status='running' AND attempt_count=?", o.Change.Status, message, completed, mysqlstore.UUID(o.Job.TenantID), mysqlstore.UUID(o.Job.ID), o.Job.AttemptCount)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ports.ErrStaleIndexJobAttempt
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO "+schema+".data_model_schema_change_log(id,tenant_id,operation,resource_type,resource_id,status,details,created_at) VALUES (?,?,?,?,?,?,?,?)", mysqlstore.UUID(o.Change.ID), mysqlstore.UUID(o.Change.TenantID), o.Change.Operation, o.Change.ResourceType, mysqlstore.UUID(o.Change.ResourceID), o.Change.Status, mysqlstore.JSON(o.Change.Details), mysqlstore.DateTime{Time: o.Change.CreatedAt})
	return err
}

func (s *IndexJobExecutionStore) readJob(ctx context.Context, tx *sql.Tx, id uuid.UUID) (datamodel.IndexJob, error) {
	job := datamodel.IndexJob{ID: id}
	var tenant mysqlstore.UUID
	var table mysqlstore.NullUUID
	var requested mysqlstore.DateTime
	var started, completed, scheduled mysqlstore.NullDateTime
	var message sql.NullString
	var columns []byte
	err := tx.QueryRowContext(ctx, "SELECT tenant_id,table_id,table_name,index_type,columns_json,status,requested_by_operation,error_message,attempt_count,requested_at,started_at,completed_at,scheduled_at,dedupe_key FROM "+s.schema+".data_model_index_jobs WHERE id=?", mysqlstore.UUID(id)).Scan(&tenant, &table, &job.TableName, &job.IndexType, &columns, &job.Status, &job.RequestedByOperation, &message, &job.AttemptCount, &requested, &started, &completed, &scheduled, &job.DedupeKey)
	if err != nil {
		return datamodel.IndexJob{}, err
	}
	var names []*string
	if err := json.Unmarshal(columns, &names); err != nil {
		return datamodel.IndexJob{}, err
	}
	if names == nil {
		return datamodel.IndexJob{}, errors.New("index job columns must be a JSON array")
	}
	job.Columns = make([]string, len(names))
	for i, name := range names {
		if name == nil {
			return datamodel.IndexJob{}, errors.New("index job column cannot be null")
		}
		job.Columns[i] = *name
	}
	job.TenantID, job.RequestedAt = uuid.UUID(tenant), requested.Time
	if table.Valid {
		value := uuid.UUID(table.UUID)
		job.TableID = &value
	}
	if started.Valid {
		job.StartedAt = &started.DateTime.Time
	}
	if completed.Valid {
		job.CompletedAt = &completed.DateTime.Time
	}
	if scheduled.Valid {
		job.ScheduledAt = &scheduled.DateTime.Time
	}
	if message.Valid {
		job.ErrorMessage = &message.String
	}
	return job, nil
}

var _ ports.IndexJobExecutionStore = (*IndexJobExecutionStore)(nil)
