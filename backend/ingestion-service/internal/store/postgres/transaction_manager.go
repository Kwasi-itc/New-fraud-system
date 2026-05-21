package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

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
func (s mutationStore) TenantWriter() ports.TenantDataWriter  { return NewTenantDataWriter(s.tx) }
