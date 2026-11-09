package mysql

import (
	"context"
	"database/sql"
	"errors"
	mysqldriver "github.com/go-sql-driver/mysql"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// IndexJobApplication composes the existing owner business rules on one local
// transaction. Returned jobs are valid only after commit. It does not execute DDL.
type IndexJobApplication struct {
	manager *TransactionManager
	read    ModelReadAuthorizer
	ids     ports.IDGenerator
	clock   ports.Clock
}

func NewIndexJobApplication(db *sql.DB, database string, tenant uuid.UUID, maxRows, maxBytes int, read ModelReadAuthorizer, write MutationAuthorizer, queue *IndexDeliveryStore, ids ports.IDGenerator, clock ports.Clock) (*IndexJobApplication, error) {
	if read == nil || queue == nil || ids == nil || clock == nil {
		return nil, errors.New("read/write authorities, durable queue, IDs and clock required")
	}
	m, err := NewTransactionManager(db, database, tenant, maxRows, maxBytes, write, queue)
	if err != nil {
		return nil, err
	}
	return &IndexJobApplication{m, read, ids, clock}, nil
}

type joinedIndexTransaction struct{ store ports.MutationStore }

func (j joinedIndexTransaction) Run(ctx context.Context, fn func(ports.MutationStore) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(j.store)
}

func (a *IndexJobApplication) owner(s ports.MutationStore) service.IndexJobService {
	return service.NewIndexJobService(s.Tenants(), s.Tables(), s.Fields(), s.IndexJobs(), s.SchemaChanges(), joinedIndexTransaction{s}, a.ids, a.clock)
}

func (a *IndexJobApplication) Create(ctx context.Context, input service.CreateIndexJobInput) (datamodel.IndexJob, error) {
	if input.TenantID != a.manager.tenant {
		return datamodel.IndexJob{}, errors.New("index job tenant mismatch")
	}
	if len(input.Columns) > a.manager.maxRows || len(input.Columns) > 100 {
		return datamodel.IndexJob{}, ErrSnapshotLimit
	}
	if err := datamodel.ValidateIndexJobCreate(input.IndexType, input.Columns); err != nil {
		return datamodel.IndexJob{}, err
	}
	var result datamodel.IndexJob
	err := a.manager.RunWithTx(ctx, func(s ports.MutationStore, tx *sql.Tx) error {
		r := s.(metadataMutationStore).repositories
		// Shared tenant lock prevents deactivation without serializing unrelated
		// requests. Table and requested fields use shared locks in stable order.
		var id mysqlstore.UUID
		if err := tx.QueryRowContext(ctx, "SELECT id FROM "+r.Tables.schema+".data_model_tenants WHERE id=? FOR SHARE", mysqlstore.UUID(input.TenantID)).Scan(&id); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, "SELECT id FROM "+r.Tables.schema+".data_model_tables WHERE tenant_id=? AND id=? AND archived=FALSE FOR SHARE", mysqlstore.UUID(input.TenantID), mysqlstore.UUID(input.TableID)).Scan(&id); err != nil {
			return err
		}
		fields, err := r.Fields.ListByTable(ctx, input.TableID)
		if err != nil {
			return err
		}
		byName := make(map[string]uuid.UUID, len(fields))
		for _, field := range fields {
			if !field.Archived {
				byName[field.Name] = field.ID
			}
		}
		args := []any{mysqlstore.UUID(input.TenantID), mysqlstore.UUID(input.TableID)}
		selected := map[uuid.UUID]bool{}
		for _, column := range input.Columns {
			field, ok := byName[datamodel.NormalizeName(column)]
			if !ok {
				return service.ErrIndexJobInvalid
			}
			if !selected[field] {
				args = append(args, mysqlstore.UUID(field))
				selected[field] = true
			}
		}
		if len(selected) > 0 {
			query := "SELECT id FROM " + r.Tables.schema + ".data_model_fields WHERE tenant_id=? AND table_id=? AND archived=FALSE AND id IN (" + strings.TrimSuffix(strings.Repeat("?,", len(selected)), ",") + ") ORDER BY id FOR SHARE"
			rows, err := tx.QueryContext(ctx, query, args...)
			if err != nil {
				return err
			}
			count := 0
			for rows.Next() {
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return err
				}
				count++
			}
			err = rows.Err()
			closeErr := rows.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			if count != len(selected) {
				return errors.New("index field changed during preparation")
			}
		}
		result, err = a.owner(s).Create(ctx, input)
		return err
	})
	if err != nil {
		// A competing preparation may have committed the same indexed intent.
		// The failed transaction is already rolled back. Reauthorize and resolve
		// that exact key in a new transaction; never replay an uncertain write.
		var duplicate *mysqldriver.MySQLError
		if errors.As(err, &duplicate) && duplicate.Number == 1062 {
			columns := make([]string, len(input.Columns))
			for i, column := range input.Columns {
				columns[i] = datamodel.NormalizeName(column)
			}
			key := datamodel.BuildIndexJobDedupeKey(input.TenantID, input.TableID, input.IndexType, columns)
			var existing *datamodel.IndexJob
			resolve := a.manager.RunWithTx(ctx, func(s ports.MutationStore, _ *sql.Tx) error {
				var e error
				existing, e = s.IndexJobs().GetByDedupeKey(ctx, key)
				return e
			})
			if resolve != nil {
				return datamodel.IndexJob{}, resolve
			}
			if existing != nil {
				return *existing, nil
			}
		}
		return datamodel.IndexJob{}, err
	}
	return result, nil
}

func (a *IndexJobApplication) Retry(ctx context.Context, id uuid.UUID) (datamodel.IndexJob, error) {
	var result datamodel.IndexJob
	err := a.manager.RunWithTx(ctx, func(s ports.MutationStore, tx *sql.Tx) error {
		store := s.(metadataMutationStore)
		jobs := store.repositories.Jobs
		if err := jobs.lock(ctx, "data_model_index_jobs", id, "", uuid.Nil); err != nil {
			return err
		}
		job, err := jobs.GetByID(ctx, id)
		if err != nil {
			return err
		}
		if job.Status != datamodel.IndexJobStatusFailed {
			return datamodel.ErrIndexJobRetryConflict
		}
		if err := a.manager.queue.prepareOwnerRetryTx(ctx, tx, job.TenantID, id); err != nil {
			return err
		}
		result, err = a.owner(s).Retry(ctx, id)
		return err
	})
	if err != nil {
		return datamodel.IndexJob{}, err
	}
	return result, nil
}

func (a *IndexJobApplication) readJobs(ctx context.Context, fn func(IndexJobRepository) error) error {
	m := a.manager
	return mysqlstore.Snapshot(ctx, m.db, func(tx *sql.Tx) error {
		if err := a.read(ctx, tx, m.tenant); err != nil {
			return err
		}
		r, err := NewMetadataRepositories(tx, m.database, m.tenant, m.maxRows, m.maxBytes)
		if err != nil {
			return err
		}
		return fn(r.Jobs)
	})
}
func (a *IndexJobApplication) ListForTables(ctx context.Context, tenant uuid.UUID, tables []uuid.UUID, limit int) ([]datamodel.IndexJob, error) {
	var result []datamodel.IndexJob
	err := a.readJobs(ctx, func(r IndexJobRepository) error {
		var err error
		result, err = r.ListByTables(ctx, tenant, tables, limit)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func (a *IndexJobApplication) List(ctx context.Context, tenant uuid.UUID) ([]datamodel.IndexJob, error) {
	var result []datamodel.IndexJob
	err := a.readJobs(ctx, func(r IndexJobRepository) error { var err error; result, err = r.ListByTenant(ctx, tenant); return err })
	if err != nil {
		return nil, err
	}
	return result, nil
}
func (a *IndexJobApplication) Get(ctx context.Context, id uuid.UUID) (datamodel.IndexJob, error) {
	var result datamodel.IndexJob
	err := a.readJobs(ctx, func(r IndexJobRepository) error { var err error; result, err = r.GetByID(ctx, id); return err })
	if err != nil {
		return datamodel.IndexJob{}, err
	}
	return result, nil
}

// prepareOwnerRetryTx is private to the failed-job owner transition. Job lock
// must already be held. Lock order is job -> delivery -> reservation, matching
// completion/cleanup. Never delete delivery evidence or reset its attempt count.
func (s *IndexDeliveryStore) prepareOwnerRetryTx(ctx context.Context, tx *sql.Tx, tenant, job uuid.UUID) error {
	var delivery mysqlstore.UUID
	err := tx.QueryRowContext(ctx, "SELECT delivery_id FROM "+s.schema+".data_model_index_delivery_reservations WHERE tenant_id=? AND job_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(job)).Scan(&delivery)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var state, queue string
	if err := tx.QueryRowContext(ctx, "SELECT state,queue_name FROM "+s.schema+".data_model_index_deliveries WHERE tenant_id=? AND job_id=? AND id=? FOR UPDATE", mysqlstore.UUID(tenant), mysqlstore.UUID(job), delivery).Scan(&state, &queue); err != nil {
		return err
	}
	if queue != s.queue {
		return datamodel.ErrIndexJobRetryConflict
	}
	switch state {
	case "available":
		return nil // Existing unleased delivery will observe the committed pending job.
	case "completed":
		_, err := tx.ExecContext(ctx, "DELETE FROM "+s.schema+".data_model_index_delivery_reservations WHERE tenant_id=? AND job_id=? AND delivery_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(job), delivery)
		return err
	default:
		return datamodel.ErrIndexJobRetryConflict
	}
}
