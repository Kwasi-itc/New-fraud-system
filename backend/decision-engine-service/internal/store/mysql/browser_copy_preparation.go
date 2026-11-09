package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/modelvalidation"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// WithCopyReader returns a separately configured admin; it never mutates an
// already shared instance or permits a different tenant/database/pool binding.
func (s *BrowserWorkflowAdmin) WithCopyReader(reader TenantDataReader) (*BrowserWorkflowAdmin, error) {
	if s == nil || s.owner == nil || !reader.MatchesPublicationScope(&AuthoringStore{configuration: s.owner.configuration}) {
		return nil, ErrTenantScope
	}
	return &BrowserWorkflowAdmin{owner: s.owner, copyReader: &reader}, nil
}

func (s *BrowserWorkflowAdmin) Copy(ctx context.Context, source, destination uuid.UUID) ([]workflow.CaseRule, error) {
	tenant, actor, err := s.binding()
	if err != nil {
		return nil, err
	}
	if s.copyReader == nil {
		return nil, ErrOwnerContractUnavailable
	}
	if source == uuid.Nil || destination == uuid.Nil || source == destination {
		return nil, ErrTenantScope
	}
	c := s.owner.configuration
	// Establish trusted authority before provider IO, then recheck on the actual
	// copy transaction. No SQL lock is held while fetching the model.
	err = mysqlstore.Snapshot(ctx, c.db, func(tx *sql.Tx) error {
		if err := s.owner.authorize(ctx, tx, c.tenant, AuthoringWorkflowRead); err != nil {
			return err
		}
		return s.copyReader.configuration.authorize(ctx, tx, c.tenant, false)
	})
	if err != nil {
		return nil, browserAuthoringError(err)
	}
	model, err := s.copyReader.ResolveExecutionModel(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(model)
	if err != nil {
		return nil, err
	}
	if len(raw) > c.maxBytes {
		return nil, ErrProjectionLimit
	}
	var detached ports.TenantModel
	if err := json.Unmarshal(raw, &detached); err != nil {
		return nil, err
	}
	return s.owner.copy(ctx, tenant, source.String(), destination.String(), actor, func(ctx context.Context, tx *sql.Tx, snapshot ScenarioCopySnapshot, target scenario.Scenario) error {
		table, ok := detached.Tables[target.TriggerObjectType]
		if !ok || table.Archived {
			return ErrPublishedModelUnavailable
		}
		for _, graph := range snapshot.Workflows {
			rule, ok := workflow.DecodeCaseRule(graph)
			if !ok {
				return workflow.ErrAuthoringConflict
			}
			if rule.Condition != nil {
				if err := validateCopiedCondition(*rule.Condition, table); err != nil {
					return err
				}
			}
		}
		guard, err := s.copyReader.executionRegistryGuard(map[string]bool{target.TriggerObjectType: true}, nil, detached)
		if err != nil {
			return err
		}
		return guard(ctx, tx)
	}, func(ctx context.Context, tx *sql.Tx) error {
		// Recheck model-read authority before inbox/tag locks on the mutation.
		return s.copyReader.configuration.authorize(ctx, tx, c.tenant, false)
	})
}

func validateCopiedCondition(tree workflow.ConditionTree, table ports.TenantModelTable) error {
	return modelvalidation.ValidateBrowserCopyCondition(tree, table)
}
