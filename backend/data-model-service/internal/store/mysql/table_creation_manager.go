package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type TableCreationAuthorizer func(context.Context, ports.TableCreationRequest) error

type TableCreationManager struct {
	metadata  TransactionManager
	authorize TableCreationAuthorizer
}

func NewTableCreationManager(metadata *TransactionManager, authorize TableCreationAuthorizer) (*TableCreationManager, error) {
	if metadata == nil || metadata.db == nil || metadata.tenant == uuid.Nil || metadata.authorize == nil || authorize == nil {
		return nil, errors.New("scoped metadata and creation authorization required")
	}
	return &TableCreationManager{*metadata, authorize}, nil
}

func (m *TableCreationManager) check(ctx context.Context, r ports.TableCreationRequest) error {
	if m == nil || r.TenantID != m.metadata.tenant {
		return errors.New("creation tenant differs from authorized scope")
	}
	if err := creationIdentity(r).validate(); err != nil {
		return err
	}
	return m.authorize(ctx, r)
}

func creationIdentity(r ports.TableCreationRequest) CreationReceiptIdentity {
	return CreationReceiptIdentity{r.OperationID, r.ActorID, r.RequestHash}
}

func (m *TableCreationManager) Allocate(ctx context.Context, r ports.TableCreationRequest, build func() (ports.TableCreationAllocation, error)) (ports.TableCreationAllocation, error) {
	if build == nil {
		return ports.TableCreationAllocation{}, errors.New("owner allocator required")
	}
	if err := m.check(ctx, r); err != nil {
		return ports.TableCreationAllocation{}, err
	}
	var result ports.TableCreationAllocation
	err := m.metadata.RunWithTx(ctx, func(owner ports.MutationStore, tx *sql.Tx) error {
		receipts, err := NewCreationReceipts(tx, m.metadata.database, r.TenantID)
		if err != nil {
			return err
		}
		previous, err := receipts.Read(ctx, creationIdentity(r))
		if err == nil {
			result = ports.TableCreationAllocation{Snapshot: previous.Snapshot, PublicationHash: previous.PublicationHash}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		active, err := owner.Tenants().GetByID(ctx, r.TenantID)
		if err != nil {
			return err
		}
		if active.Status != tenant.StatusActive {
			return errors.New("tenant must be active before allocating table creation")
		}
		proposed, err := build()
		if err != nil {
			return err
		}
		saved, err := receipts.Reserve(ctx, creationIdentity(r), proposed.Snapshot, proposed.PublicationHash)
		if err != nil {
			return err
		}
		if !saved.Replayed {
			if err := owner.Tables().Create(ctx, saved.Snapshot.Table); err != nil {
				return err
			}
			for _, f := range saved.Snapshot.Fields {
				if err := owner.Fields().Create(ctx, f); err != nil {
					return err
				}
			}
			registry, err := NewPhysicalRegistry(tx, m.metadata.database, r.TenantID, m.metadata.maxRows)
			if err != nil {
				return err
			}
			if err := registry.RegisterTable(ctx, saved.Snapshot.Table.ID); err != nil {
				return err
			}
			if err := registry.RegisterFields(ctx, saved.Snapshot.Table.ID, []uuid.UUID{saved.Snapshot.Fields[0].ID, saved.Snapshot.Fields[1].ID}); err != nil {
				return err
			}
		}
		result = ports.TableCreationAllocation{Snapshot: saved.Snapshot, PublicationHash: saved.PublicationHash}
		return nil
	})
	if err != nil {
		return ports.TableCreationAllocation{}, err
	}
	return result, nil
}

// BindPlan joins a complete isolated-reference-bound plan to the retained
// allocation. Reference capture happens before this transaction, never while
// holding row locks or by adopting the application target's observed shape.
func (m *TableCreationManager) BindPlan(ctx context.Context, r ports.TableCreationRequest, plan PhysicalDDLPlan) (DDLIntentResult, error) {
	if err := m.check(ctx, r); err != nil {
		return DDLIntentResult{}, err
	}
	if plan.Version != 2 || plan.Creation == nil {
		return DDLIntentResult{}, ErrPhysicalPlanningUnsupported
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		return DDLIntentResult{}, err
	}
	if len(raw) > 1<<20 {
		return DDLIntentResult{}, ErrSnapshotLimit
	}
	intents, err := NewDDLIntentStore(m.metadata.db, m.metadata.database, r.TenantID, 1<<20)
	if err != nil {
		return DDLIntentResult{}, err
	}
	var result DDLIntentResult
	err = m.metadata.RunWithTx(ctx, func(owner ports.MutationStore, tx *sql.Tx) error {
		receipts, err := NewCreationReceipts(tx, m.metadata.database, r.TenantID)
		if err != nil {
			return err
		}
		saved, err := receipts.Read(ctx, creationIdentity(r))
		if err != nil {
			return err
		}
		t := saved.Snapshot.Table
		schema, _ := mysqlstore.Identifier(m.metadata.database)
		var locked mysqlstore.UUID
		if err := tx.QueryRowContext(ctx, "SELECT id FROM "+schema+".data_model_tables WHERE tenant_id=? AND id=? FOR UPDATE", mysqlstore.UUID(r.TenantID), mysqlstore.UUID(t.ID)).Scan(&locked); err != nil {
			return err
		}
		var name string
		if err := tx.QueryRowContext(ctx, "SELECT physical_name FROM "+schema+".data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(r.TenantID), mysqlstore.UUID(t.ID)).Scan(&name); err != nil {
			return err
		}
		decoded, err := decodePhysicalPlan(raw, m.metadata.database, name)
		if err != nil {
			return err
		}
		c := decoded.Creation
		if c.TenantID != r.TenantID || c.TableID != t.ID || c.ObjectFieldID != saved.Snapshot.Fields[0].ID || c.UpdatedFieldID != saved.Snapshot.Fields[1].ID {
			return ErrPhysicalConflict
		}
		registry, err := NewPhysicalRegistry(tx, m.metadata.database, r.TenantID, 3)
		if err != nil {
			return err
		}
		fields, err := registry.Fields(ctx, t.ID)
		if err != nil {
			return err
		}
		if len(fields) != 2 {
			return ErrPhysicalConflict
		}
		for _, f := range fields {
			if f.Component != "value" || (f.FieldID == c.ObjectFieldID && f.Name != c.ObjectColumn) || (f.FieldID == c.UpdatedFieldID && f.Name != c.UpdatedColumn) || (f.FieldID != c.ObjectFieldID && f.FieldID != c.UpdatedFieldID) {
				return ErrPhysicalConflict
			}
		}
		result, err = intents.PrepareTx(ctx, tx, DDLIntent{TableID: t.ID, OperationID: r.OperationID, RequestHash: saved.PublicationHash, ActorKind: "api", ActorID: r.ActorID.String(), Shape: raw})
		return err
	})
	if err != nil {
		return DDLIntentResult{}, err
	}
	return result, nil
}

func (m *TableCreationManager) Recover(ctx context.Context, r ports.TableCreationRequest) (datamodel.Table, error) {
	if err := m.check(ctx, r); err != nil {
		return datamodel.Table{}, err
	}
	var result datamodel.Table
	err := m.metadata.RunWithTx(ctx, func(_ ports.MutationStore, tx *sql.Tx) error {
		receipts, err := NewCreationReceipts(tx, m.metadata.database, r.TenantID)
		if err != nil {
			return err
		}
		result, err = receipts.PublishedTable(ctx, creationIdentity(r))
		return err
	})
	if err != nil {
		return datamodel.Table{}, err
	}
	return result, nil
}

var _ ports.TableCreationManager = (*TableCreationManager)(nil)
