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
	db           *pgxpool.Pool
	uploadJobs   riverjobs.UploadLogEnqueuer
	deferredJobs riverjobs.DeferredIngestEnqueuer
}

// WithJobEnqueuers binds backend-specific producers to this transaction manager.
// Only the adapter sees pgx.Tx; services enqueue through the mutation store.
func (m TransactionManager) WithJobEnqueuers(upload riverjobs.UploadLogEnqueuer, deferred riverjobs.DeferredIngestEnqueuer) TransactionManager {
	m.uploadJobs, m.deferredJobs = upload, deferred
	return m
}

func NewTransactionManager(db *pgxpool.Pool) TransactionManager {
	return TransactionManager{db: db}
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
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tx.Rollback(cleanup); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			result = errors.Join(result, fmt.Errorf("rollback ingestion transaction: %w", err))
		}
	}()

	store := mutationStore{tx: tx, uploadJobs: m.uploadJobs, deferredJobs: m.deferredJobs}
	if err := fn(store); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

type mutationStore struct {
	tx           pgx.Tx
	uploadJobs   riverjobs.UploadLogEnqueuer
	deferredJobs riverjobs.DeferredIngestEnqueuer
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
	if s.uploadJobs == nil {
		return ports.ErrJobQueueUnavailable
	}
	return s.uploadJobs.EnqueueTx(ctx, s.tx, id, at)
}
func (s mutationStore) EnqueueDeferredIngest(ctx context.Context, id uuid.UUID, at *time.Time) error {
	if s.deferredJobs == nil {
		return ports.ErrJobQueueUnavailable
	}
	return s.deferredJobs.EnqueueTx(ctx, s.tx, id, at)
}
