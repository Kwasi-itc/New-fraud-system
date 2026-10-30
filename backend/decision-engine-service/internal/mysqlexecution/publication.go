package mysqlexecution

import (
	"context"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
)

// Publication coordinates the real full AST validator and registered SQL
// capability checks. Provider IO and expensive static validation finish before
// the publication write transaction; authority, parent binding and published
// model gates are rechecked inside it. No network reads run under owner locks.
type Publication struct {
	owner  *mysql.AuthoringStore
	reader mysql.TenantDataReader
	limits service.ExecutionPreparationLimits
}

func NewPublication(owner *mysql.AuthoringStore, reader mysql.TenantDataReader, limits service.ExecutionPreparationLimits) (*Publication, error) {
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	if !reader.MatchesPublicationScope(owner) {
		return nil, mysql.ErrTenantScope
	}
	return &Publication{owner, reader, limits}, nil
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
