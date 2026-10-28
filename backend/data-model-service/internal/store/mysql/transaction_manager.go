package mysql

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// MutationAuthorizer must validate trusted identity, current write permission
// and resource ownership for this tenant in the same transaction. It must use
// local SQL, never network calls. A constructor's tenant UUID is not authority.
type MutationAuthorizer func(context.Context, *sql.Tx, uuid.UUID) error

// TransactionManager is staged metadata-only infrastructure. It is not selected
// by runtime and does not implement publication or the physical DDL protocol.
type TransactionManager struct {
	db                *sql.DB
	database          string
	tenant            uuid.UUID
	maxRows, maxBytes int
	authorize         MutationAuthorizer
	queue             *IndexDeliveryStore
}

func NewTransactionManager(db *sql.DB, database string, tenant uuid.UUID, maxRows, maxBytes int, authorize MutationAuthorizer, queue *IndexDeliveryStore) (*TransactionManager, error) {
	if tenant == uuid.Nil {
		return nil, errors.New("nonzero tenant scope required")
	}
	m, err := newTransactionConfiguration(db, database, maxRows, maxBytes, authorize, queue)
	if err != nil {
		return nil, err
	}
	m.tenant = tenant
	return m, nil
}

func newTransactionConfiguration(db *sql.DB, database string, maxRows, maxBytes int, authorize MutationAuthorizer, queue *IndexDeliveryStore) (*TransactionManager, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if db == nil || maxRows < 1 || maxRows > 100000 || maxBytes < 1 || maxBytes > 64<<20 || authorize == nil {
		return nil, errors.New("database, scoped tenant, bounded limits and mutation authorizer required")
	}
	if queue != nil && (queue.db != db || queue.schema != schema) {
		return nil, errors.New("delivery store must use the same pool and database")
	}
	return &TransactionManager{db, database, uuid.Nil, maxRows, maxBytes, authorize, queue}, nil
}

func (m *TransactionManager) Run(ctx context.Context, fn func(ports.MutationStore) error) error {
	if fn == nil {
		return errors.New("mutation callback required")
	}
	return m.RunWithTx(ctx, func(store ports.MutationStore, _ *sql.Tx) error { return fn(store) })
}

// RunWithTx allows MySQL owner adapters to compose publication on this exact
// transaction. Results are provisional until RunWithTx returns nil. Do not
// commit, roll back, issue DDL/session changes, or retain the store/transaction.
// CommitError requires operation-receipt reconciliation, never blind retry.
func (m *TransactionManager) RunWithTx(ctx context.Context, fn func(ports.MutationStore, *sql.Tx) error) error {
	if m == nil || m.db == nil || m.tenant == uuid.Nil || m.authorize == nil || fn == nil {
		return errors.New("initialized transaction manager and callback required")
	}
	return mysqlstore.Mutation(ctx, m.db, func(tx *sql.Tx) error {
		if err := m.authorize(ctx, tx, m.tenant); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		repositories, err := NewMetadataRepositories(tx, m.database, m.tenant, m.maxRows, m.maxBytes)
		if err != nil {
			return err
		}
		return fn(metadataMutationStore{repositories, tx, m.tenant, m.queue}, tx)
	})
}

type metadataMutationStore struct {
	repositories MetadataRepositories
	tx           *sql.Tx
	tenant       uuid.UUID
	queue        *IndexDeliveryStore
}

func (s metadataMutationStore) Tenants() ports.TenantRepository { return s.repositories.Tenants }
func (s metadataMutationStore) Tables() ports.TableRepository   { return s.repositories.Tables }
func (s metadataMutationStore) Fields() ports.FieldRepository   { return s.repositories.Fields }
func (s metadataMutationStore) FieldEnumValues() ports.FieldEnumValueRepository {
	return s.repositories.Enums
}
func (s metadataMutationStore) Links() ports.LinkRepository   { return s.repositories.Links }
func (s metadataMutationStore) Pivots() ports.PivotRepository { return s.repositories.Pivots }
func (s metadataMutationStore) TableOptions() ports.TableOptionsRepository {
	return s.repositories.Options
}
func (s metadataMutationStore) NavigationOptions() ports.NavigationOptionRepository {
	return s.repositories.Navigation
}
func (s metadataMutationStore) SchemaChanges() ports.SchemaChangeRepository {
	return s.repositories.History
}
func (s metadataMutationStore) TenantSchemaMigrations() ports.TenantSchemaMigrationRepository {
	return s.repositories.Revisions
}
func (s metadataMutationStore) IndexJobs() ports.IndexJobRepository { return s.repositories.Jobs }
func (s metadataMutationStore) SchemaManager() ports.SchemaManager {
	return metadataOnlySchemaManager{}
}
func (s metadataMutationStore) EnqueueIndexJob(ctx context.Context, id uuid.UUID, at *time.Time) error {
	if s.queue == nil {
		return ports.ErrJobQueueUnavailable
	}
	_, err := s.queue.EnqueueTx(ctx, s.tx, s.tenant, id, at)
	return err
}

var _ ports.TransactionManager = (*TransactionManager)(nil)
var _ ports.MutationStore = metadataMutationStore{}
