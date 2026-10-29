package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/riverjobs"
)

type TransactionManager struct {
	db       *pgxpool.Pool
	uploads  riverjobs.UploadLogEnqueuer
	deferred riverjobs.DeferredIngestEnqueuer
}

func NewTransactionManager(db *pgxpool.Pool) TransactionManager {
	return TransactionManager{db: db}
}

// WithEnqueuers binds backend producers to this adapter's transaction. Domain
// services never receive a driver transaction. Missing required producers fail
// the mutation explicitly, rather than saving unscheduled work successfully.
func (m TransactionManager) WithEnqueuers(uploads riverjobs.UploadLogEnqueuer, deferred riverjobs.DeferredIngestEnqueuer) TransactionManager {
	m.uploads, m.deferred = uploads, deferred
	return m
}

func (m TransactionManager) Run(ctx context.Context, fn func(ports.MutationStore) error) (result error) {
	if m.db == nil || fn == nil {
		return errors.New("database and transaction callback are required")
	}
	tx, err := m.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := tx.Rollback(cleanup); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			result = errors.Join(result, fmt.Errorf("rollback ingestion transaction: %w", err))
		}
	}()

	store := mutationStore{tx: tx, uploads: m.uploads, deferred: m.deferred}
	if err := fn(store); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type mutationStore struct {
	tx       pgx.Tx
	uploads  riverjobs.UploadLogEnqueuer
	deferred riverjobs.DeferredIngestEnqueuer
}

func (s mutationStore) RecordIngestion(ctx context.Context, audit ingestion.IngestionAudit, event ingestion.OutboxEvent) error {
	batch := &pgx.Batch{}
	batch.Queue(insertAuditSQL, auditArgs(audit)...)
	batch.Queue(insertOutboxSQL, outboxArgs(event)...)
	// Close drains results and propagates either statement's error. The outer
	// transaction rolls back the record as well as both supporting writes.
	return s.tx.SendBatch(ctx, batch).Close()
}

func (s mutationStore) Audits() ports.IngestionAuditRepository {
	return NewIngestionAuditRepository(s.tx)
}
func (s mutationStore) Idempotency() ports.IdempotencyRepository {
	return NewIdempotencyRepository(s.tx)
}
func (s mutationStore) OutboxEvents() ports.OutboxEventRepository {
	return NewOutboxEventRepository(s.tx)
}
func (s mutationStore) UploadLogs() ports.UploadLogRepository { return NewUploadLogRepository(s.tx) }
func (s mutationStore) DeferredIngests() ports.DeferredIngestRepository {
	return NewDeferredIngestRepository(s.tx)
}
func (s mutationStore) TenantWriter() ports.TenantDataWriter { return NewTenantDataWriter(s.tx) }
func (s mutationStore) TenantReader() ports.TenantDataReader { return NewTenantDataReader(s.tx) }
func (s mutationStore) EnqueueUploadLog(ctx context.Context, id uuid.UUID, at *time.Time) error {
	if s.tx == nil || s.uploads == nil || id == uuid.Nil {
		return errors.New("upload enqueue requires a transaction, producer and resource identity")
	}
	return s.uploads.EnqueueTx(ctx, s.tx, id, at)
}
func (s mutationStore) EnqueueDeferredIngest(ctx context.Context, id uuid.UUID, at *time.Time) error {
	if s.tx == nil || s.deferred == nil || id == uuid.Nil {
		return errors.New("deferred enqueue requires a transaction, producer and resource identity")
	}
	return s.deferred.EnqueueTx(ctx, s.tx, id, at)
}

func (s mutationStore) RetryUploadLog(ctx context.Context, id uuid.UUID, at *time.Time) error {
	if s.tx == nil || s.uploads == nil || id == uuid.Nil {
		return errors.New("upload retry requires a transaction, producer and resource identity")
	}
	var attempt int
	if err := s.tx.QueryRow(ctx, "SELECT attempt_count FROM core_ingestion.upload_logs WHERE id=$1 FOR UPDATE", id).Scan(&attempt); err != nil {
		return err
	}
	return s.uploads.RetryTx(ctx, s.tx, id, attempt, at)
}
func (s mutationStore) RetryDeferredIngest(ctx context.Context, id uuid.UUID, at *time.Time) error {
	if s.tx == nil || s.deferred == nil || id == uuid.Nil {
		return errors.New("deferred retry requires a transaction, producer and resource identity")
	}
	var attempt int
	if err := s.tx.QueryRow(ctx, "SELECT attempt_count FROM core_ingestion.deferred_ingests WHERE id=$1 FOR UPDATE", id).Scan(&attempt); err != nil {
		return err
	}
	return s.deferred.RetryTx(ctx, s.tx, id, attempt, at)
}
