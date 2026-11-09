package mysqlexecution

import (
	"context"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/google/uuid"
)

// Publication coordinates the real full AST validator and registered SQL
// capability checks. Provider IO and expensive static validation finish before
// the publication write transaction; authority, parent binding and published
// model gates are rechecked inside it. No network reads run under owner locks.
type Publication struct {
	owner   *mysql.AuthoringStore
	reader  mysql.TenantDataReader
	limits  service.ExecutionPreparationLimits
	indexes ports.IndexPreparationProvider
}

func (p *Publication) WithIndexPreparation(owner ports.IndexPreparationProvider) *Publication {
	copy := *p
	copy.indexes = owner
	return &copy
}

func (p *Publication) Preparation(ctx context.Context, parent, iteration uuid.UUID, start bool) (scenario.PublicationPreparationStatus, error) {
	if p == nil || p.indexes == nil {
		return scenario.PublicationPreparationStatus{}, mysql.ErrOwnerContractUnavailable
	}
	snapshot, err := p.owner.IndexPreparationSnapshot(ctx, parent, iteration)
	if err != nil {
		return scenario.PublicationPreparationStatus{}, err
	}
	model, err := p.reader.ResolveAuthorizedPreparationModel(ctx)
	if err != nil {
		return scenario.PublicationPreparationStatus{}, err
	}
	model, err = service.CloneExecutionModel(model, p.limits.MaxBytes)
	if err != nil {
		return scenario.PublicationPreparationStatus{}, err
	}
	return service.PreparePublicationIndexes(ctx, snapshot.Scenario, snapshot.Iteration, snapshot.Rules, model, p.indexes, start, p.limits)
}

// MatchesExecutionStore prevents a request factory from combining publication
// authority with an execution endpoint from a different tenant/pool/database.
func (p *Publication) MatchesExecutionStore(owner *mysql.ExecutionStore) bool {
	return p != nil && p.reader.MatchesExecutionPreparationScope(owner)
}

func (p *Publication) Unpublish(ctx context.Context, input mysql.PublicationInput) ([]scenario.Publication, error) {
	if p == nil || p.owner == nil {
		return nil, mysql.ErrDecisionAuthorizationUnavailable
	}
	return p.owner.Unpublish(ctx, input)
}

// History uses the authorized bounded snapshot reader; an overflow is an error,
// never a silently truncated publication history.
func (p *Publication) History(ctx context.Context, tenant, parent uuid.UUID) (out []scenario.Publication, err error) {
	if p == nil || p.owner == nil || tenant == uuid.Nil || parent == uuid.Nil {
		return nil, mysql.ErrTenantScope
	}
	err = p.owner.ReadSnapshot(ctx, func(r mysql.AuthoringRepositories) error {
		if _, err := r.Scenarios.GetByID(ctx, tenant.String(), parent.String()); err != nil {
			return err
		}
		var err error
		out, err = r.Publications.ListByScenario(ctx, tenant.String(), parent.String())
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (p *Publication) HistoryPage(ctx context.Context, tenant, parent uuid.UUID, f mysql.PublicationPageFilter) (out mysql.PublicationPage, err error) {
	if p == nil || p.owner == nil || tenant == uuid.Nil || parent == uuid.Nil {
		return out, mysql.ErrTenantScope
	}
	err = p.owner.ReadSnapshot(ctx, func(r mysql.AuthoringRepositories) error {
		if _, err := r.Scenarios.GetByID(ctx, tenant.String(), parent.String()); err != nil {
			return err
		}
		var err error
		out, err = r.Publications.Page(ctx, tenant.String(), parent.String(), f)
		return err
	})
	if err != nil {
		return mysql.PublicationPage{}, err
	}
	return out, nil
}

func NewPublication(owner *mysql.AuthoringStore, reader mysql.TenantDataReader, limits service.ExecutionPreparationLimits) (*Publication, error) {
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	if !reader.MatchesPublicationScope(owner) {
		return nil, mysql.ErrTenantScope
	}
	return &Publication{owner: owner, reader: reader, limits: limits}, nil
}

func (p *Publication) Publish(ctx context.Context, input mysql.PublicationInput) ([]scenario.Publication, error) {
	if p == nil || p.owner == nil {
		return nil, mysql.ErrDecisionAuthorizationUnavailable
	}
	snapshot, err := p.owner.PublicationPreparation(ctx, input.ScenarioID, input.IterationID)
	if err != nil {
		return nil, err
	}
	pin, err := service.NewPinnedScenarioSnapshot(snapshot.Scenario, snapshot.Iteration, snapshot.Rules, p.limits)
	if err != nil {
		return nil, err
	}
	model, err := p.reader.ResolveExecutionModel(ctx)
	if err != nil {
		return nil, err
	}
	model, err = service.CloneExecutionModel(model, p.limits.MaxBytes)
	if err != nil {
		return nil, err
	}
	if err := service.ValidatePinnedModel(pin, model); err != nil {
		return nil, err
	}
	binding, err := p.reader.PreparePublicationBinding(ctx, p.owner, snapshot, model)
	if err != nil {
		return nil, err
	}
	return p.owner.PublishPrepared(ctx, input, binding)
}
