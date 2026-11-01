package mysql

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// PublicationSnapshot is immutable for this transition: parent is exclusively
// locked and a committed iteration and its rules cannot be edited. The guard
// must validate the current published model/AST/index preparation using reviewed
// local owner read contracts on tx. It must not call the data-model HTTP API
// while locks are held. Nil never means "preparation not required".
type PublicationSnapshot struct {
	Scenario  scenario.Scenario
	Iteration scenario.Iteration
	Rules     []scenario.Rule
}
type PublicationGuard func(context.Context, *sql.Tx, PublicationSnapshot) error
type PublicationInput struct{ ScenarioID, IterationID, EventID, ReplacedEventID uuid.UUID }

func (s *AuthoringStore) publication(ctx context.Context, input PublicationInput, unpublish bool, guard PublicationGuard, beforeLocks func(context.Context, *sql.Tx) error) ([]scenario.Publication, error) {
	if s == nil || s.configuration == nil || s.authorize == nil {
		return nil, errors.New("initialized authoring store required")
	}
	if input.ScenarioID == uuid.Nil || input.IterationID == uuid.Nil || input.EventID == uuid.Nil || (!unpublish && (input.ReplacedEventID == uuid.Nil || input.ReplacedEventID == input.EventID)) {
		return nil, ErrTenantScope
	}
	if !unpublish && guard == nil {
		return nil, errors.New("current local publication/model preparation guard required")
	}
	c := s.configuration
	var result []scenario.Publication
	err := mysqlstore.Mutation(ctx, c.db, func(tx *sql.Tx) error {
		if err := s.authorize(ctx, tx, c.tenant, AuthoringPublish); err != nil {
			return err
		}
		if beforeLocks != nil {
			if err := beforeLocks(ctx, tx); err != nil {
				return err
			}
		}
		base := authoringRepository{platformRepository: platformRepository{tx, c.schema, c.tenant, c.maxRows, c.maxBytes, true}, publication: true}
		repo := ScenarioRepository{base}
		tenant, parent, iteration := c.tenant.String(), input.ScenarioID.String(), input.IterationID.String()
		args, _ := base.ids(tenant, parent)
		var found mysqlstore.UUID
		if err := tx.QueryRowContext(ctx, "SELECT id FROM "+c.schema+".decision_engine_scenarios WHERE tenant_id=? AND id=? FOR UPDATE", args...).Scan(&found); err != nil {
			return err
		}
		current, err := repo.GetByID(ctx, tenant, parent)
		if err != nil {
			return err
		}
		if unpublish && (current.LiveIterationID == nil || *current.LiveIterationID != iteration) {
			return errors.New("iteration is not currently live")
		}
		if !unpublish {
			bindings, _ := base.ids(tenant, parent, iteration)
			if err := tx.QueryRowContext(ctx, "SELECT id FROM "+c.schema+".decision_engine_scenario_iterations WHERE tenant_id=? AND scenario_id=? AND id=? FOR SHARE", bindings...).Scan(&found); err != nil {
				return err
			}
			it, err := (ScenarioIterationRepository{base}).GetByID(ctx, tenant, parent, iteration)
			if err != nil {
				return err
			}
			if it.Status != scenario.IterationStatusCommitted {
				return errors.New("iteration must be committed before publish")
			}
			rules, err := (RuleRepository{base}).ListByIteration(ctx, tenant, parent, iteration)
			if err != nil {
				return err
			}
			if err := guard(ctx, tx, PublicationSnapshot{current, it, rules}); err != nil {
				return err
			}
		}
		var at mysqlstore.DateTime
		if err := tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&at); err != nil {
			return err
		}
		events := ScenarioPublicationRepository{base}
		appendEvent := func(id uuid.UUID, iteration string, action scenario.PublicationAction) error {
			p, err := events.Create(ctx, scenario.Publication{ID: id.String(), TenantID: tenant, ScenarioID: parent, IterationID: iteration, Action: action, CreatedAt: at.Time})
			if err == nil {
				result = append(result, p)
			}
			return err
		}
		if unpublish {
			if err := repo.SetLiveIterationID(ctx, tenant, parent, nil); err != nil {
				return err
			}
			return appendEvent(input.EventID, iteration, scenario.PublicationActionUnpublish)
		}
		if current.LiveIterationID != nil && *current.LiveIterationID != iteration {
			if err := appendEvent(input.ReplacedEventID, *current.LiveIterationID, scenario.PublicationActionUnpublish); err != nil {
				return err
			}
		}
		if err := repo.SetLiveIterationID(ctx, tenant, parent, &iteration); err != nil {
			return err
		}
		return appendEvent(input.EventID, iteration, scenario.PublicationActionPublish)
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func (s *AuthoringStore) Publish(ctx context.Context, input PublicationInput, guard PublicationGuard) ([]scenario.Publication, error) {
	return s.publication(ctx, input, false, guard, nil)
}
func (s *AuthoringStore) Unpublish(ctx context.Context, input PublicationInput) ([]scenario.Publication, error) {
	return s.publication(ctx, input, true, nil, nil)
}

// CloneIteration copies configuration and ordered rules under a source freeze;
// the source and destination are the SAME tenant/scenario. The result is draft,
// preserving stable_rule_id and all formula/threshold values. Workflow/scenario
// copying is a different operation. No automatic retry after uncertain commit.
func (s *AuthoringStore) CloneIteration(ctx context.Context, parent, source, destination uuid.UUID) (scenario.Iteration, error) {
	if parent == uuid.Nil || source == uuid.Nil || destination == uuid.Nil || source == destination {
		return scenario.Iteration{}, ErrTenantScope
	}
	var result scenario.Iteration
	err := s.Mutate(ctx, func(r AuthoringRepositories) error {
		tenant := s.configuration.tenant.String()
		version, err := r.Iterations.NextVersion(ctx, tenant, parent.String())
		if err != nil {
			return err
		}
		args, _ := r.Iterations.ids(tenant, parent.String(), source.String())
		var found mysqlstore.UUID
		if err := r.Iterations.tx.QueryRowContext(ctx, "SELECT id FROM "+r.Iterations.schema+".decision_engine_scenario_iterations WHERE tenant_id=? AND scenario_id=? AND id=? FOR UPDATE", args...).Scan(&found); err != nil {
			return err
		}
		item, err := r.Iterations.GetByID(ctx, tenant, parent.String(), source.String())
		if err != nil {
			return err
		}
		rules, err := r.Rules.ListByIteration(ctx, tenant, parent.String(), source.String())
		if err != nil {
			return err
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		item.ID = destination.String()
		item.Version = version
		item.Status = scenario.IterationStatusDraft
		item.CommittedAt = nil
		item.CreatedAt = now
		result, err = r.Iterations.Create(ctx, item)
		if err != nil {
			return err
		}
		for i := range rules {
			rules[i].ID = uuid.NewString()
			rules[i].IterationID = destination.String()
			rules[i].CreatedAt = now
			rules[i].UpdatedAt = now
		}
		_, err = r.Rules.CreateMany(ctx, tenant, parent.String(), destination.String(), rules)
		return err
	})
	if err != nil {
		return scenario.Iteration{}, err
	}
	return result, nil
}
