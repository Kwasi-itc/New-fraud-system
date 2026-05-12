package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Kwasi-itc/marble-datamodel-service/internal/ports"
	tenantdbpostgres "github.com/Kwasi-itc/marble-datamodel-service/internal/tenantdb/postgres"
)

type TransactionManager struct {
	db *pgxpool.Pool
}

func NewTransactionManager(db *pgxpool.Pool) TransactionManager {
	return TransactionManager{db: db}
}

func (m TransactionManager) Run(ctx context.Context, fn func(ports.MutationStore) error) error {
	tx, err := m.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	store := mutationStore{
		tenants:       NewTenantRepository(tx),
		tables:        NewTableRepository(tx),
		fields:        NewFieldRepository(tx),
		links:         NewLinkRepository(tx),
		pivots:        NewPivotRepository(tx),
		tableOptions:  NewTableOptionsRepository(tx),
		schemaChanges: NewSchemaChangeRepository(tx),
		schemaManager: tenantdbpostgres.NewSchemaManager(tx),
	}

	if err := fn(store); err != nil {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
			return fmt.Errorf("rollback transaction after %v: %w", err, rollbackErr)
		}
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

type mutationStore struct {
	tenants       ports.TenantRepository
	tables        ports.TableRepository
	fields        ports.FieldRepository
	links         ports.LinkRepository
	pivots        ports.PivotRepository
	tableOptions  ports.TableOptionsRepository
	schemaChanges ports.SchemaChangeRepository
	schemaManager ports.SchemaManager
}

func (s mutationStore) Tenants() ports.TenantRepository             { return s.tenants }
func (s mutationStore) Tables() ports.TableRepository               { return s.tables }
func (s mutationStore) Fields() ports.FieldRepository               { return s.fields }
func (s mutationStore) Links() ports.LinkRepository                 { return s.links }
func (s mutationStore) Pivots() ports.PivotRepository               { return s.pivots }
func (s mutationStore) TableOptions() ports.TableOptionsRepository  { return s.tableOptions }
func (s mutationStore) SchemaChanges() ports.SchemaChangeRepository { return s.schemaChanges }
func (s mutationStore) SchemaManager() ports.SchemaManager          { return s.schemaManager }
