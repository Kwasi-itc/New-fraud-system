package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
)

type TransactionManager struct {
	db *pgxpool.Pool
}

func NewTransactionManager(db *pgxpool.Pool) TransactionManager {
	return TransactionManager{db: db}
}

func (m TransactionManager) Run(ctx context.Context, fn func(store ports.MutationStore) error) error {
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

func (s mutationStore) Screenings() ports.ScreeningRepository {
	return NewScreeningRepository(s.tx)
}

func (s mutationStore) ScreeningMatches() ports.ScreeningMatchRepository {
	return NewScreeningMatchRepository(s.tx)
}

func (s mutationStore) ScreeningComments() ports.ScreeningCommentRepository {
	return NewScreeningCommentRepository(s.tx)
}

func (s mutationStore) Whitelist() ports.ScreeningWhitelistRepository {
	return NewScreeningWhitelistRepository(s.tx)
}

func (s mutationStore) ScreeningFiles() ports.ScreeningFileRepository {
	return NewScreeningFileRepository(s.tx)
}

func (s mutationStore) ContinuousConfigs() ports.ContinuousConfigRepository {
	return NewContinuousConfigRepository(s.tx)
}

func (s mutationStore) MonitoredObjects() ports.MonitoredObjectRepository {
	return NewMonitoredObjectRepository(s.tx)
}

func (s mutationStore) DatasetUpdateJobs() ports.DatasetUpdateJobRepository {
	return NewDatasetUpdateJobRepository(s.tx)
}
