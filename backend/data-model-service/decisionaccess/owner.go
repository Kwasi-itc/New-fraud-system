// Package decisionaccess exposes a bounded, staged owner API on a shared pool.
// Mutations pass through the data-model service's transaction and business rules.
package decisionaccess

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/google/uuid"
)

type Authorize func(context.Context, *sql.Tx, uuid.UUID) error
type IDs interface{ New() uuid.UUID }
type Clock interface{ Now() time.Time }
type Config struct {
	DB                *sql.DB
	Database          string
	Tenant            uuid.UUID
	MaxRows, MaxBytes int
	Queue             string
	MaxAttempts       int
	Lease             time.Duration
	Read, Write       Authorize
	IDs               IDs
	Clock             Clock
}
type Owner struct {
	db       *sql.DB
	database string
	tenant   uuid.UUID
	model    service.DataModelReadService
	jobs     *store.IndexJobApplication
}
type Job struct {
	ID, TenantID, TableID, TableName, IndexType, Status string
	Columns                                             []string
}

func New(c Config) (*Owner, error) {
	reader, err := store.NewAuthorizedPublishedModelReader(c.DB, c.Database, c.Tenant, c.MaxRows, c.MaxBytes, store.ModelReadAuthorizer(c.Read))
	if err != nil {
		return nil, err
	}
	model, err := service.NewSnapshotDataModelReadService(reader)
	if err != nil {
		return nil, err
	}
	queue, err := store.NewIndexDeliveryStore(c.DB, c.Database, c.Queue, c.MaxAttempts, c.Lease)
	if err != nil {
		return nil, err
	}
	jobs, err := store.NewIndexJobApplication(c.DB, c.Database, c.Tenant, c.MaxRows, c.MaxBytes, store.ModelReadAuthorizer(c.Read), store.MutationAuthorizer(c.Write), queue, c.IDs, c.Clock)
	if err != nil {
		return nil, err
	}
	return &Owner{c.DB, c.Database, c.Tenant, model, jobs}, nil
}
func (o *Owner) Matches(db *sql.DB, database string) bool {
	return o != nil && o.db == db && o.database == database
}
func (o *Owner) tenantID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil || id != o.tenant {
		return uuid.Nil, errors.New("model owner tenant mismatch")
	}
	return id, nil
}

// ModelJSON uses the same versioned DTO as the owner's HTTP endpoint. Reads are
// always fresh and carry the eligible physical publication token.
func (o *Owner) ModelJSON(ctx context.Context, tenant string) ([]byte, error) {
	id, err := o.tenantID(tenant)
	if err != nil {
		return nil, err
	}
	m, err := o.model.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"data_model": dto.AdaptAssembledDataModel(m.Model, m.RevisionID, m.Tenant.Status)})
}
func adapt(j datamodel.IndexJob) Job {
	r := Job{ID: j.ID.String(), TenantID: j.TenantID.String(), TableName: j.TableName, IndexType: string(j.IndexType), Status: string(j.Status), Columns: append([]string(nil), j.Columns...)}
	if j.TableID != nil {
		r.TableID = j.TableID.String()
	}
	return r
}
func (o *Owner) List(ctx context.Context, tenant string, tables []string, limit int) ([]Job, error) {
	id, err := o.tenantID(tenant)
	if err != nil {
		return nil, err
	}
	if len(tables) > 500 {
		return nil, errors.New("model owner table scope exceeds limit")
	}
	ids := make([]uuid.UUID, len(tables))
	for i, raw := range tables {
		ids[i], err = uuid.Parse(raw)
		if err != nil {
			return nil, err
		}
	}
	jobs, err := o.jobs.ListForTables(ctx, id, ids, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Job, len(jobs))
	for i, j := range jobs {
		out[i] = adapt(j)
	}
	return out, nil
}
func (o *Owner) Create(ctx context.Context, tenant, table, kind string, columns []string, operation string) (Job, error) {
	id, err := o.tenantID(tenant)
	if err != nil {
		return Job{}, err
	}
	tid, err := uuid.Parse(table)
	if err != nil {
		return Job{}, err
	}
	j, err := o.jobs.Create(ctx, service.CreateIndexJobInput{TenantID: id, TableID: tid, IndexType: datamodel.IndexJobType(kind), Columns: append([]string(nil), columns...), RequestedByOperation: operation})
	if err != nil {
		return Job{}, err
	}
	return adapt(j), nil
}
func (o *Owner) Retry(ctx context.Context, job string) error {
	id, err := uuid.Parse(job)
	if err != nil {
		return err
	}
	_, err = o.jobs.Retry(ctx, id)
	return err
}
