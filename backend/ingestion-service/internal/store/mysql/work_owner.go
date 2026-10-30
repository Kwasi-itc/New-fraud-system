package mysql

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/work"
	"github.com/google/uuid"
)

const UploadWork = "upload_log_execute"
const DeferredWork = "deferred_ingest_execute"

var ErrWorkIdentity = errors.New("ingestion execution identity differs from original request")

// WorkSchema keeps ingestion resource state separate from shared queue mechanics.
// Worker receipts have a separate namespace: public caller keys cannot collide
// with internally generated execution identities, even when their bytes match.
func WorkSchema(database string) ([]string, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	ddl := []string{"CREATE TABLE " + s + `.ingestion_work_resources (
id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,table_id BINARY(16) NOT NULL,
object_type LONGTEXT NOT NULL,kind VARCHAR(48) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
mode VARBINARY(6) NOT NULL,payload LONGBLOB NOT NULL,request_key LONGBLOB NULL,
filename LONGTEXT NOT NULL,content_type LONGTEXT NOT NULL,scheduled_at DATETIME(6) NOT NULL,
result_payload JSON NULL,total_rows INT NOT NULL DEFAULT 0,successful_rows INT NOT NULL DEFAULT 0,failed_rows INT NOT NULL DEFAULT 0,
created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),completed_at DATETIME(6) NULL,
UNIQUE(tenant_id,id),KEY history(tenant_id,table_id,created_at DESC,id DESC),
KEY upload_history(tenant_id,table_id,kind,created_at DESC,id DESC),
FOREIGN KEY(tenant_id,table_id) REFERENCES ` + s + `.data_model_tables(tenant_id,id),
CHECK(kind IN ('upload_log_execute','deferred_ingest_execute')),CHECK(mode IN ('create','patch')),
CHECK(total_rows>=0 AND successful_rows>=0 AND failed_rows>=0 AND successful_rows+failed_rows<=total_rows)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin`}
	jobs, err := work.Schema(database, "ingestion")
	if err != nil {
		return nil, err
	}
	ddl = append(ddl, jobs...)
	ddl = append(ddl, "ALTER TABLE "+s+".ingestion_work_jobs ADD CONSTRAINT ingestion_work_resource_fk FOREIGN KEY(tenant_id,resource_id) REFERENCES "+s+".ingestion_work_resources(tenant_id,id)")
	receipts, err := ReceiptSchema(database)
	if err != nil {
		return nil, err
	}
	for _, statement := range receipts {
		ddl = append(ddl, strings.NewReplacer("ingestion_receipt_buckets", "ingestion_execution_buckets", "ingestion_request_receipts", "ingestion_execution_receipts", "ingestion_receipt_bucket_fk", "ingestion_execution_bucket_fk").Replace(statement))
	}
	return ddl, nil
}

type WorkInput struct {
	ID                    uuid.UUID
	Kind                  string
	Mode                  ingestion.Mode
	Payload               []byte
	RequestKey            *string
	Filename, ContentType string
	ScheduledAt           time.Time
}
type WorkResult struct {
	ID                                             uuid.UUID
	Kind, State                                    string
	Attempt, TotalRows, SuccessfulRows, FailedRows int
	Result                                         json.RawMessage
	CreatedAt                                      time.Time
	StartedAt                                      *time.Time
	CompletedAt                                    *time.Time
}
type WorkOwner struct {
	owner                      *KeyedRecordOwner
	queue                      *work.Queue
	maxPayload, maxAttempts    int
	uploadQueue, deferredQueue string
}

func NewWorkOwner(owner *KeyedRecordOwner, uploadQueue, deferredQueue string, maxPayload, maxAttempts int) (*WorkOwner, error) {
	if owner == nil || maxPayload < 1 || maxPayload > 16<<20 || maxAttempts < 1 || maxAttempts > 100 {
		return nil, errors.New("record owner and bounded work policy required")
	}
	q, err := work.New(owner.receipts.db, owner.database, "ingestion", 4096)
	if err != nil {
		return nil, err
	}
	// Queue validation is shared with enqueue; reject invalid config before use.
	if !work.ValidQueueName(uploadQueue) || !work.ValidQueueName(deferredQueue) {
		return nil, errors.New("invalid ingestion queue name")
	}
	return &WorkOwner{owner, q, maxPayload, maxAttempts, uploadQueue, deferredQueue}, nil
}
func (w *WorkOwner) Queue() *work.Queue { return w.queue }
func (w *WorkOwner) scope(mode ingestion.Mode, kind string) ReceiptRequest {
	response := "single"
	if kind == UploadWork {
		response = "batch"
	}
	return ReceiptRequest{TenantID: w.owner.receipts.tenant, TableID: w.owner.table, Mode: string(mode), Kind: response}
}

// Create requires a stable execution UUID for ambiguous-commit reconciliation.
// Duplicate handling checks this exact primary key and every immutable value;
// it never updates a row reached through an unrelated unique constraint.
func (w *WorkOwner) Create(ctx context.Context, in WorkInput) (WorkResult, error) {
	if w == nil || in.ID == uuid.Nil || (in.Kind != UploadWork && in.Kind != DeferredWork) || (in.Mode != ingestion.ModeCreate && in.Mode != ingestion.ModePatch) || len(in.Payload) > w.maxPayload || len(in.Payload) == 0 || !utf8.ValidString(in.Filename) || !utf8.ValidString(in.ContentType) || len(in.Filename)+len(in.ContentType) > 4096 {
		return WorkResult{}, errors.New("bounded ingestion work required")
	}
	if in.RequestKey != nil && (*in.RequestKey == "" || len(*in.RequestKey) > w.owner.limits.Receipt.KeyBytes) {
		return WorkResult{}, errors.New("invalid caller request key")
	}
	if in.Kind == UploadWork && in.RequestKey != nil {
		return WorkResult{}, errors.New("upload uses private execution identity")
	}
	if _, err := (mysqlstore.DateTime{Time: in.ScheduledAt}).Value(); err != nil {
		return WorkResult{}, err
	}
	if in.Kind == DeferredWork {
		var raw map[string]any
		decoder := json.NewDecoder(bytes.NewReader(in.Payload))
		decoder.UseNumber()
		if !utf8.Valid(in.Payload) || !json.Valid(in.Payload) {
			return WorkResult{}, errors.New("invalid deferred JSON")
		}
		if err := decoder.Decode(&raw); err != nil || raw == nil {
			return WorkResult{}, errors.New("deferred payload must be an object")
		}
	}
	in.Payload = bytes.Clone(in.Payload)
	if in.RequestKey != nil {
		k := *in.RequestKey
		in.RequestKey = &k
	}
	scope := w.scope(in.Mode, in.Kind)
	if err := w.owner.receipts.authorize(ctx, scope); err != nil {
		return WorkResult{}, err
	}
	s := w.owner.receipts.schema
	err := mysqlstore.Mutation(ctx, w.owner.receipts.db, func(tx *sql.Tx) error {
		if err := w.owner.receipts.authorizeTx(ctx, tx, scope); err != nil {
			return err
		}
		_, insertErr := tx.ExecContext(ctx, "INSERT INTO "+s+".ingestion_work_resources(id,tenant_id,table_id,object_type,kind,mode,payload,request_key,filename,content_type,scheduled_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)", mysqlstore.UUID(in.ID), mysqlstore.UUID(scope.TenantID), mysqlstore.UUID(scope.TableID), w.owner.objectType, in.Kind, string(in.Mode), in.Payload, in.RequestKey, in.Filename, in.ContentType, mysqlstore.DateTime{Time: in.ScheduledAt})
		if insertErr != nil {
			if mysqlstore.Classify(insertErr) != mysqlstore.Duplicate {
				return insertErr
			}
			var same bool
			err := tx.QueryRowContext(ctx, "SELECT tenant_id=? AND table_id=? AND BINARY object_type=? AND kind=? AND mode=? AND payload=? AND request_key<=>? AND BINARY filename=? AND BINARY content_type=? AND scheduled_at=? FROM "+s+".ingestion_work_resources WHERE id=? FOR UPDATE", mysqlstore.UUID(scope.TenantID), mysqlstore.UUID(scope.TableID), []byte(w.owner.objectType), in.Kind, string(in.Mode), in.Payload, in.RequestKey, []byte(in.Filename), []byte(in.ContentType), mysqlstore.DateTime{Time: in.ScheduledAt}, mysqlstore.UUID(in.ID)).Scan(&same)
			if err != nil {
				return errors.Join(insertErr, err)
			}
			if !same {
				return ErrWorkIdentity
			}
			// A retained resource without its work row is inconsistent, not success.
			var at mysqlstore.DateTime
			var queue string
			var attempts int
			if err := tx.QueryRowContext(ctx, "SELECT available_at,queue,max_attempts FROM "+s+".ingestion_work_jobs WHERE tenant_id=? AND id=?", mysqlstore.UUID(scope.TenantID), mysqlstore.UUID(in.ID)).Scan(&at, &queue, &attempts); err != nil {
				return err
			}
			return nil
		}
		queue := w.deferredQueue
		if in.Kind == UploadWork {
			queue = w.uploadQueue
		}
		return w.queue.EnqueueTx(ctx, tx, work.Job{ID: in.ID, TenantID: scope.TenantID, ResourceID: in.ID, Kind: in.Kind, Queue: queue, Version: 1, Payload: json.RawMessage(`{}`), AvailableAt: in.ScheduledAt, MaxAttempts: w.maxAttempts})
	})
	if err != nil {
		return WorkResult{}, err
	}
	return w.Get(ctx, in.ID)
}

// Get is tenant/table scoped and projects status/results, never raw upload data.
func (w *WorkOwner) Get(ctx context.Context, id uuid.UUID) (WorkResult, error) {
	var result WorkResult
	scope, err := w.resourceScope(ctx, id)
	if err != nil {
		return result, err
	}
	if err := w.owner.receipts.authorize(ctx, scope); err != nil {
		return result, err
	}
	err = mysqlstore.Mutation(ctx, w.owner.receipts.db, func(tx *sql.Tx) error {
		if err := w.owner.receipts.authorizeTx(ctx, tx, scope); err != nil {
			return err
		}
		var created mysqlstore.DateTime
		var completed, started mysqlstore.NullDateTime
		var payload []byte
		var size sql.NullInt64
		err := tx.QueryRowContext(ctx, "SELECT r.kind,j.state,j.attempt,r.total_rows,r.successful_rows,r.failed_rows,r.created_at,j.started_at,COALESCE(r.completed_at,j.finished_at),OCTET_LENGTH(r.result_payload),CASE WHEN OCTET_LENGTH(r.result_payload)<=? THEN r.result_payload ELSE NULL END FROM "+w.owner.receipts.schema+".ingestion_work_resources r JOIN "+w.owner.receipts.schema+".ingestion_work_jobs j ON j.tenant_id=r.tenant_id AND j.resource_id=r.id AND j.id=COALESCE(r.current_job_id,r.id) WHERE r.tenant_id=? AND r.table_id=? AND r.id=?", w.owner.limits.Receipt.ResultBytes, mysqlstore.UUID(scope.TenantID), mysqlstore.UUID(scope.TableID), mysqlstore.UUID(id)).Scan(&result.Kind, &result.State, &result.Attempt, &result.TotalRows, &result.SuccessfulRows, &result.FailedRows, &created, &started, &completed, &size, &payload)
		if err != nil {
			return err
		}
		if size.Valid && size.Int64 > int64(w.owner.limits.Receipt.ResultBytes) {
			return ErrReceiptLimit
		}
		result.ID = id
		result.CreatedAt = created.Time
		// Queued retry DTOs retain the source contract: no active attempt start.
		// The queue still retains the previous claim time for operational accounting.
		if started.Valid && result.State != "available" {
			at := started.DateTime.Time
			result.StartedAt = &at
		}
		result.Result = payload
		if completed.Valid {
			at := completed.DateTime.Time
			result.CompletedAt = &at
		}
		return nil
	})
	if err != nil {
		return WorkResult{}, err
	}
	return result, nil
}

// Handle performs parsing outside locks and fences the work row before any
// receipt/publication/object lock. Completion and all local effects are atomic.
func (w *WorkOwner) Handle(ctx context.Context, l work.Lease) error {
	if l.TenantID != w.owner.receipts.tenant || l.Version != 1 || (l.Kind != UploadWork && l.Kind != DeferredWork) {
		return work.ErrLease
	}
	var payload []byte
	var mode string
	var key []byte
	scope, err := w.resourceScope(ctx, l.ResourceID)
	if err != nil {
		return err
	}
	if err := w.owner.receipts.authorize(ctx, scope); err != nil {
		return err
	}
	err = mysqlstore.Mutation(ctx, w.owner.receipts.db, func(tx *sql.Tx) error {
		if err := w.queue.FenceTx(ctx, tx, l); err != nil {
			return err
		}
		if err := w.owner.receipts.authorizeTx(ctx, tx, scope); err != nil {
			return err
		}
		var size int
		if err := tx.QueryRowContext(ctx, "SELECT mode,OCTET_LENGTH(payload),CASE WHEN OCTET_LENGTH(payload)<=? THEN payload ELSE NULL END,request_key FROM "+w.owner.receipts.schema+".ingestion_work_resources WHERE tenant_id=? AND table_id=? AND id=? AND kind=?", w.maxPayload, mysqlstore.UUID(l.TenantID), mysqlstore.UUID(w.owner.table), mysqlstore.UUID(l.ResourceID), l.Kind).Scan(&mode, &size, &payload, &key); err != nil {
			return err
		}
		if size > w.maxPayload {
			return errors.New("retained work exceeds payload bound")
		}
		return nil
	})
	if err != nil {
		return err
	}
	var records []map[string]any
	if l.Kind == UploadWork {
		records, err = decodeWorkCSV(payload)
	} else {
		var raw map[string]any
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.UseNumber()
		err = decoder.Decode(&raw)
		if err == nil {
			var extra any
			if decoder.Decode(&extra) != io.EOF {
				err = errors.New("one deferred object required")
			}
		}
		if err == nil && raw == nil {
			err = errors.New("deferred object required")
		}
		records = []map[string]any{raw}
	}
	if err != nil {
		return w.failMalformed(ctx, l, scope)
	}
	command := ingestion.ReceiptCommand{TenantID: l.TenantID, TableID: w.owner.table, Mode: ingestion.Mode(mode), Kind: "single", Records: records}
	if l.Kind == UploadWork {
		command.Kind = "batch"
	}
	ownerCopy := *w.owner
	if key == nil {
		manager := *w.owner.receipts
		manager.bucketTable = manager.schema + ".ingestion_execution_buckets"
		manager.receiptTable = manager.schema + ".ingestion_execution_receipts"
		ownerCopy.receipts = &manager
		key = []byte(l.ResourceID.String())
	}
	finish := func(ctx context.Context, tx *sql.Tx, payload []byte, failed bool) error {
		success, failure := len(records), 0
		if failed {
			success, failure = 0, len(records)
		}
		var response any
		if payload != nil {
			response = string(payload)
		}
		_, err := tx.ExecContext(ctx, "UPDATE "+w.owner.receipts.schema+".ingestion_work_resources SET result_payload=?,total_rows=?,successful_rows=?,failed_rows=?,completed_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND table_id=? AND id=?", response, len(records), success, failure, mysqlstore.UUID(l.TenantID), mysqlstore.UUID(w.owner.table), mysqlstore.UUID(l.ResourceID))
		if err != nil {
			return err
		}
		if failed {
			return w.queue.DiscardTx(ctx, tx, l, "validation_failed")
		}
		return w.queue.CompleteTx(ctx, tx, l)
	}
	hooks := recordHooks{before: func(ctx context.Context, tx *sql.Tx) error { return w.queue.FenceTx(ctx, tx, l) },
		after: func(ctx context.Context, tx *sql.Tx, r ReceiptResult) error { return finish(ctx, tx, r.Payload, false) },
		invalid: func(ctx context.Context, tx *sql.Tx, _ *RecordValidationError) error {
			return finish(ctx, tx, nil, true)
		}}
	_, err = ownerCopy.execute(ctx, command, string(key), hooks)
	var validation *RecordValidationError
	if errors.As(err, &validation) && err == validation {
		return nil
	}
	return err
}

func (w *WorkOwner) failMalformed(ctx context.Context, l work.Lease, scope ReceiptRequest) error {
	return mysqlstore.Mutation(ctx, w.owner.receipts.db, func(tx *sql.Tx) error {
		if err := w.queue.FenceTx(ctx, tx, l); err != nil {
			return err
		}
		if err := w.owner.receipts.authorizeTx(ctx, tx, scope); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE "+w.owner.receipts.schema+".ingestion_work_resources SET completed_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND table_id=? AND id=?", mysqlstore.UUID(l.TenantID), mysqlstore.UUID(w.owner.table), mysqlstore.UUID(l.ResourceID)); err != nil {
			return err
		}
		return w.queue.DiscardTx(ctx, tx, l, "invalid_payload")
	})
}

// Immutable resource metadata is loaded by exact tenant/table/ID before current
// authorization. It is never returned to an unauthorized caller or used as an
// authorization substitute. Read access currently requires the original write
// scope; a future independent read policy must be wired explicitly.
func (w *WorkOwner) resourceScope(ctx context.Context, id uuid.UUID) (ReceiptRequest, error) {
	var mode, kind string
	err := w.owner.receipts.db.QueryRowContext(ctx, "SELECT mode,kind FROM "+w.owner.receipts.schema+".ingestion_work_resources WHERE tenant_id=? AND table_id=? AND id=?", mysqlstore.UUID(w.owner.receipts.tenant), mysqlstore.UUID(w.owner.table), mysqlstore.UUID(id)).Scan(&mode, &kind)
	if err != nil {
		return ReceiptRequest{}, err
	}
	return w.scope(ingestion.Mode(mode), kind), nil
}

func decodeWorkCSV(payload []byte) ([]map[string]any, error) {
	if !utf8.Valid(payload) {
		return nil, errors.New("CSV must be UTF-8")
	}
	r := csv.NewReader(bytes.NewReader(payload))
	headers, err := r.Read()
	if err != nil {
		return nil, err
	}
	if len(headers) < 1 || len(headers) > 1000 {
		return nil, errors.New("CSV column bound")
	}
	seen := map[string]bool{}
	for _, h := range headers {
		if h == "" || seen[h] {
			return nil, errors.New("empty or duplicate CSV header")
		}
		seen[h] = true
	}
	var result []map[string]any
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(result) >= 500 {
			return nil, errors.New("CSV exceeds 500 rows")
		}
		value := make(map[string]any, len(headers))
		for i, h := range headers {
			s := strings.TrimSpace(row[i])
			if s == "" {
				value[h] = nil
			} else {
				value[h] = s
			}
		}
		result = append(result, value)
	}
	if len(result) == 0 {
		return nil, errors.New("CSV has no records")
	}
	return result, nil
}
