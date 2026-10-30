package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// WorkflowCopyGuard sees the actual destination, whose trigger/model may differ
// from the source. Case/routing/tag authorization and condition compatibility
// must be checked against that destination before any authoring locks or writes.
type WorkflowCopyGuard func(context.Context, *sql.Tx, ScenarioCopySnapshot, scenario.Scenario) error

// CopyWorkflowRules copies only the active structured graph, not source history,
// decisions, effects or legacy configuration. Every source read shares one short
// repeatable snapshot; destination authorization and case/tag preparation are
// rechecked before writes. Partial insertion/guard errors roll back everything.
// No retries or replay success are inferred without a durable copy receipt.
func (s *ConfigurationStore) CopyWorkflowRules(ctx context.Context, source, destination uuid.UUID, dependencies WorkflowCopyGuard) ([]workflow.StructuredRule, error) {
	return s.copyWorkflowRules(ctx, source, destination, dependencies, nil, nil)
}

// Hooks are private, local SQL steps for browser bounds/audit composition. They
// cannot commit independently; a failed audit rolls back the copied graph.
func (s *ConfigurationStore) copyWorkflowRules(ctx context.Context, source, destination uuid.UUID, dependencies WorkflowCopyGuard, before func(context.Context, authoringRepository, []workflow.StructuredRule) error, after func(context.Context, authoringRepository, []workflow.StructuredRule) error) ([]workflow.StructuredRule, error) {
	if s == nil || s.authoring == nil || s.authoring.configuration == nil || dependencies == nil {
		return nil, errors.New("configuration store and current dependency guard required")
	}
	if source == uuid.Nil || destination == uuid.Nil || source == destination {
		return nil, ErrTenantScope
	}
	c := s.authoring.configuration
	var snapshot ScenarioCopySnapshot
	err := mysqlstore.Snapshot(ctx, c.db, func(tx *sql.Tx) error {
		if err := s.authoring.authorize(ctx, tx, c.tenant, AuthoringWorkflowRead); err != nil {
			return err
		}
		r := authoringRepository{platformRepository: platformRepository{tx, c.schema, c.tenant, c.maxRows, c.maxBytes, false}}
		var err error
		snapshot.Scenario, err = (ScenarioRepository{r}).GetByID(ctx, c.tenant.String(), source.String())
		if err != nil {
			return err
		}
		snapshot.Workflows, err = (WorkflowRuleRepository{r}).ListStructured(ctx, c.tenant.String(), source.String())
		if err != nil {
			return err
		}
		return copySnapshotBudget(snapshot, c.maxRows, c.maxBytes)
	})
	if err != nil {
		return nil, err
	}
	var result []workflow.StructuredRule
	err = mysqlstore.Mutation(ctx, c.db, func(tx *sql.Tx) error {
		if err := s.authoring.authorize(ctx, tx, c.tenant, AuthoringWorkflowWrite); err != nil {
			return err
		}
		r := authoringRepository{platformRepository: platformRepository{tx, c.schema, c.tenant, c.maxRows, c.maxBytes, true}, workflowGuards: make(map[uuid.UUID]bool)}
		target, err := (ScenarioRepository{r}).GetByID(ctx, c.tenant.String(), destination.String())
		if err != nil {
			return err
		}
		if err := dependencies(ctx, tx, snapshot, target); err != nil {
			return err
		}
		// Dependency checks precede owner locks. Now fence a destination edit
		// that could otherwise change its trigger/model after validation.
		var found mysqlstore.UUID
		if err := tx.QueryRowContext(ctx, "SELECT id FROM "+c.schema+".decision_engine_scenarios WHERE tenant_id=? AND id=? AND updated_at=? AND CAST(trigger_object_type AS BINARY)=CAST(? AS BINARY) FOR SHARE", mysqlstore.UUID(c.tenant), mysqlstore.UUID(destination), mysqlstore.DateTime{Time: target.UpdatedAt}, mysqlstore.Text(target.TriggerObjectType)).Scan(&found); err != nil {
			return err
		}
		// Source live state is irrelevant for a workflow-only copy.
		snapshot.Scenario.LiveIterationID = nil
		var now mysqlstore.DateTime
		if err := tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&now); err != nil {
			return err
		}
		copy, err := reidentifyCopy(snapshot, ScenarioCopyInput{SourceID: source, DestinationID: destination}, now.Time)
		if err != nil {
			return err
		}
		if before != nil {
			if err := before(ctx, r, copy.Workflows); err != nil {
				return err
			}
		}
		if err := writeCopiedWorkflows(ctx, r, copy.Workflows); err != nil {
			return err
		}
		if after != nil {
			if err := after(ctx, r, copy.Workflows); err != nil {
				return err
			}
		}
		result = copy.Workflows
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
