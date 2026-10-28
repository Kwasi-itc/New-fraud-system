package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/access"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/riverjobs"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	tenantdbpostgres "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/tenantdb/postgres"
)

type TransactionManager struct {
	db       *pgxpool.Pool
	enqueuer riverjobs.IndexJobEnqueuer
}

func NewTransactionManager(db *pgxpool.Pool) TransactionManager {
	return TransactionManager{db: db}
}

// WithIndexJobEnqueuer binds the backend producer to transactions owned here.
func (m TransactionManager) WithIndexJobEnqueuer(e riverjobs.IndexJobEnqueuer) TransactionManager {
	m.enqueuer = e
	return m
}

func (m TransactionManager) Run(ctx context.Context, fn func(ports.MutationStore) error) error {
	if fn == nil {
		return errors.New("transaction callback is required")
	}
	return m.RunWithTx(ctx, func(store ports.MutationStore, _ pgx.Tx) error { return fn(store) })
}

// RunWithTx is for PostgreSQL adapters that execute owner SQL alongside domain
// mutations (currently display settings). Domain services use Run instead.
func (m TransactionManager) RunWithTx(ctx context.Context, fn func(ports.MutationStore, pgx.Tx) error) (result error) {
	if m.db == nil || fn == nil {
		return errors.New("database and transaction callback are required")
	}
	tx, err := m.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		// Request cancellation must not prevent cleanup or hide the operation error.
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tx.Rollback(cleanup); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			result = errors.Join(result, fmt.Errorf("rollback data-model transaction: %w", err))
		}
	}()
	if grant, ok := access.FromContext(ctx); ok {
		if !grant.Write {
			return access.Denied()
		}
		if err := access.Check(ctx, tx, grant, true); err != nil {
			return err
		}
	}

	store := mutationStore{
		tenants:                NewTenantRepository(tx),
		tables:                 NewTableRepository(tx),
		fields:                 NewFieldRepository(tx),
		fieldEnumValues:        NewFieldEnumValueRepository(tx),
		links:                  NewLinkRepository(tx),
		pivots:                 NewPivotRepository(tx),
		tableOptions:           NewTableOptionsRepository(tx),
		navigationOptions:      NewNavigationOptionRepository(tx),
		schemaChanges:          NewSchemaChangeRepository(tx),
		tenantSchemaMigrations: NewTenantSchemaMigrationRepository(tx),
		indexJobs:              NewIndexJobRepository(tx),
		schemaManager:          tenantdbpostgres.NewSchemaManager(tx),
		rawTx:                  tx,
		enqueuer:               m.enqueuer,
	}

	if err := fn(store, tx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

type mutationStore struct {
	tenants                ports.TenantRepository
	tables                 ports.TableRepository
	fields                 ports.FieldRepository
	fieldEnumValues        ports.FieldEnumValueRepository
	links                  ports.LinkRepository
	pivots                 ports.PivotRepository
	tableOptions           ports.TableOptionsRepository
	navigationOptions      ports.NavigationOptionRepository
	schemaChanges          ports.SchemaChangeRepository
	tenantSchemaMigrations ports.TenantSchemaMigrationRepository
	indexJobs              ports.IndexJobRepository
	schemaManager          ports.SchemaManager
	rawTx                  pgx.Tx
	enqueuer               riverjobs.IndexJobEnqueuer
}

func (s mutationStore) Tenants() ports.TenantRepository { return s.tenants }
func (s mutationStore) Tables() ports.TableRepository   { return s.tables }
func (s mutationStore) Fields() ports.FieldRepository   { return s.fields }
func (s mutationStore) FieldEnumValues() ports.FieldEnumValueRepository {
	return s.fieldEnumValues
}
func (s mutationStore) Links() ports.LinkRepository                { return s.links }
func (s mutationStore) Pivots() ports.PivotRepository              { return s.pivots }
func (s mutationStore) TableOptions() ports.TableOptionsRepository { return s.tableOptions }
func (s mutationStore) NavigationOptions() ports.NavigationOptionRepository {
	return s.navigationOptions
}
func (s mutationStore) SchemaChanges() ports.SchemaChangeRepository { return s.schemaChanges }
func (s mutationStore) TenantSchemaMigrations() ports.TenantSchemaMigrationRepository {
	return s.tenantSchemaMigrations
}
func (s mutationStore) IndexJobs() ports.IndexJobRepository { return s.indexJobs }
func (s mutationStore) SchemaManager() ports.SchemaManager  { return s.schemaManager }
func (s mutationStore) EnqueueIndexJob(ctx context.Context, id uuid.UUID, at *time.Time) error {
	if s.enqueuer == nil {
		return ports.ErrJobQueueUnavailable
	}
	return s.enqueuer.EnqueueTx(ctx, s.rawTx, id, at)
}
