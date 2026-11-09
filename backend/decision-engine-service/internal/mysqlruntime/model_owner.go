package mysqlruntime

import (
	"context"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/decisionaccess"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/clients/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
)

// BindModelOwner connects both capabilities to one real owner on the same pool
// and database. No endpoint discovery, legacy fallback or cross-owner SQL writes
// are involved. Each operation rechecks the owner's supplied trusted authority.
func BindModelOwner(deps Dependencies, owner *decisionaccess.Owner) (Dependencies, error) {
	if !owner.Matches(deps.DB, deps.Database) {
		return Dependencies{}, mysql.ErrOwnerContractUnavailable
	}
	adapter := localModelOwner{owner}
	deps.Models = adapter
	deps.IndexPreparation = adapter
	return deps, nil
}

type localModelOwner struct{ owner *decisionaccess.Owner }

// PublishedRecordKeyContract selects the shipped owner v005 read contract.
// Missing owner migration/grants fail closed; no legacy lookup is attempted.
func (m localModelOwner) PublishedRecordKeyContract() string { return "data_model_v005" }

func (m localModelOwner) GetTenantModel(ctx context.Context, tenant string) (ports.TenantModel, error) {
	return m.GetFreshTenantModel(ctx, tenant)
}
func (m localModelOwner) GetFreshTenantModel(ctx context.Context, tenant string) (ports.TenantModel, error) {
	raw, err := m.owner.ModelJSON(ctx, tenant)
	if err != nil {
		return ports.TenantModel{}, err
	}
	return datamodel.DecodeTenantModel(raw)
}
func ownerJob(j decisionaccess.Job) ports.ManagedIndexJob {
	return ports.ManagedIndexJob{ID: j.ID, TenantID: j.TenantID, TableID: j.TableID, TableName: j.TableName, IndexType: j.IndexType, Status: j.Status, Columns: append([]string(nil), j.Columns...)}
}
func (m localModelOwner) ListIndexJobsForTables(ctx context.Context, tenant string, tables []string, limit int) ([]ports.ManagedIndexJob, error) {
	jobs, err := m.owner.List(ctx, tenant, tables, limit)
	if err != nil {
		return nil, err
	}
	out := make([]ports.ManagedIndexJob, len(jobs))
	for i, j := range jobs {
		out[i] = ownerJob(j)
	}
	return out, nil
}
func (m localModelOwner) CreateIndexJob(ctx context.Context, tenant, table, kind string, columns []string, operation string) (ports.ManagedIndexJob, error) {
	j, err := m.owner.Create(ctx, tenant, table, kind, columns, operation)
	if err != nil {
		return ports.ManagedIndexJob{}, err
	}
	return ownerJob(j), nil
}
func (m localModelOwner) RetryIndexJob(ctx context.Context, id string) error {
	return m.owner.Retry(ctx, id)
}

var _ ports.TenantModelReader = localModelOwner{}
var _ ports.FreshTenantModelReader = localModelOwner{}
var _ ports.IndexPreparationProvider = localModelOwner{}
