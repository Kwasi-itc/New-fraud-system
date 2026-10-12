package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
)

type TransactionManager struct {
	db *pgxpool.Pool
}

func NewTransactionManager(db *pgxpool.Pool) TransactionManager {
	return TransactionManager{db: db}
}

func (m TransactionManager) Run(ctx context.Context, fn func(ports.MutationStore) error) error {
	tx, err := m.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	store := mutationStore{tx: tx}
	if err := fn(store); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

type mutationStore struct {
	tx pgx.Tx
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
func (s mutationStore) RawTx() pgx.Tx                        { return s.tx }
