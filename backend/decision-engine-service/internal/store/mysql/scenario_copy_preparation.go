package mysql

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// ResolveAuthorizedPreparationModel checks read authority before external model IO.
// The copy guard rechecks that authority on the eventual mutation transaction.
func (r TenantDataReader) ResolveAuthorizedPreparationModel(ctx context.Context) (ports.TenantModel, error) {
	if r.configuration == nil {
		return ports.TenantModel{}, ErrTenantScope
	}
	c := r.configuration
	if err := mysqlstore.Snapshot(ctx, c.db, func(tx *sql.Tx) error {
		return c.authorize(ctx, tx, c.tenant, false)
	}); err != nil {
		return ports.TenantModel{}, err
	}
	return r.ResolveExecutionModel(ctx)
}

// ScenarioCopyRegistryGuard precompiles all source iterations and workflow
// payload expressions. It locks each required table once, in table-ID order.
// Portable AST validation must precede this target capability preparation.
func (r TenantDataReader) ScenarioCopyRegistryGuard(ctx context.Context, snapshot ScenarioCopySnapshot, model ports.TenantModel, permissions ScenarioCopyGuard) (ScenarioCopyGuard, error) {
	if permissions == nil {
		return nil, ErrOwnerContractUnavailable
	}
	if r.configuration == nil || snapshot.Scenario.TenantID != r.configuration.tenant.String() {
		return nil, ErrTenantScope
	}
	c := r.configuration
	grouped := map[string][]scenario.Rule{}
	for _, rule := range snapshot.Rules {
		grouped[rule.IterationID] = append(grouped[rule.IterationID], rule)
	}
	names := map[string]bool{snapshot.Scenario.TriggerObjectType: true}
	var queries []ports.AggregateQuery
	add := func(p PublicationSnapshot) error {
		n, err := publicationRelatedTables(p, model, c.maxBytes)
		if err != nil {
			return err
		}
		for name := range n {
			names[name] = true
		}
		q, err := aggregatePublicationQueries(ctx, p, model, c.maxRows, c.maxBytes)
		if err != nil {
			return err
		}
		queries = append(queries, q...)
		if len(queries) > c.maxRows {
			return ErrProjectionLimit
		}
		return nil
	}
	for _, iteration := range snapshot.Iterations {
		if err := add(PublicationSnapshot{snapshot.Scenario, iteration, grouped[iteration.ID]}); err != nil {
			return nil, err
		}
	}
	for _, graph := range snapshot.Workflows {
		for _, condition := range graph.Conditions {
			if condition.Function == workflow.ConditionTreeMatch {
				var tree workflow.ConditionTree
				if err := json.Unmarshal(condition.Params, &tree); err != nil {
					return nil, err
				}
				table, ok := model.Tables[snapshot.Scenario.TriggerObjectType]
				if !ok || table.Archived {
					return nil, ErrPublishedModelUnavailable
				}
				if err := validateCopiedCondition(tree, table); err != nil {
					return nil, err
				}
			}
			if condition.Function != workflow.ConditionPayloadEvaluates {
				continue
			}
			var params struct {
				Expression json.RawMessage `json:"expression"`
			}
			if err := json.Unmarshal(condition.Params, &params); err != nil {
				return nil, err
			}
			if err := add(PublicationSnapshot{Scenario: snapshot.Scenario, Iteration: scenario.Iteration{TriggerFormula: params.Expression}}); err != nil {
				return nil, err
			}
		}
	}
	guard, err := r.executionRegistryGuard(names, queries, model)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, tx *sql.Tx, current ScenarioCopySnapshot) error {
		if current.Scenario.ID != snapshot.Scenario.ID || current.Scenario.TenantID != c.tenant.String() {
			return ErrTenantScope
		}
		if err := c.authorize(ctx, tx, c.tenant, false); err != nil {
			return err
		}
		// Identity/model authority precedes case-owner locks, which in turn
		// precede registry locks, matching browser workflow-copy ordering.
		if err := permissions(ctx, tx, current); err != nil {
			return err
		}
		return guard(ctx, tx)
	}, nil
}

// ScenarioCopyPermissions enforces case ownership for every case destination.
// Non-case actions retain explicit dispatcher configuration; copying sends no IO.
func (s *AuthoringStore) ScenarioCopyPermissions(session *TrustedDecisionSession, snapshot ScenarioCopySnapshot) (ScenarioCopyGuard, error) {
	if s == nil || s.configuration == nil {
		return nil, ErrTenantScope
	}
	inboxes, tags := map[uuid.UUID]bool{}, map[uuid.UUID]bool{}
	for _, graph := range snapshot.Workflows {
		for _, action := range graph.Actions {
			if err := action.Validate(); err != nil {
				return nil, err
			}
			if !workflow.IsCaseAction(action.ActionType) {
				continue
			}
			var cfg struct {
				InboxID string   `json:"inbox_id"`
				TagIDs  []string `json:"tag_ids"`
			}
			if err := json.Unmarshal(action.ActionConfig, &cfg); err != nil {
				return nil, err
			}
			inboxes[uuid.MustParse(cfg.InboxID)] = true
			for _, id := range cfg.TagIDs {
				tags[uuid.MustParse(id)] = true
			}
		}
	}
	if len(inboxes) == 0 {
		return func(context.Context, *sql.Tx, ScenarioCopySnapshot) error { return nil }, nil // No case references to authorize.
	}
	if session == nil || session.TenantID != s.configuration.tenant {
		return nil, ErrDecisionAuthorizationUnavailable
	}
	if len(inboxes) > 500 || len(tags) > s.configuration.maxRows {
		return nil, ErrProjectionLimit
	}
	database := s.configuration.schema[1 : len(s.configuration.schema)-1]
	auth, err := SessionAuthorizer(database, *session)
	if err != nil {
		return nil, err
	}
	box, err := NewInboxContract(database, *session)
	if err != nil {
		return nil, err
	}
	tag, err := NewTagContract(database, *session)
	if err != nil {
		return nil, err
	}
	boxIDs, tagIDs := sortedWorkflowIDs(inboxes), sortedWorkflowIDs(tags)
	return func(ctx context.Context, tx *sql.Tx, _ ScenarioCopySnapshot) error {
		tenant := s.configuration.tenant
		if err := auth(ctx, tx, tenant, AuthoringWorkflowWrite); err != nil {
			return err
		}
		if err := box.AuthorizeBatch(ctx, tx, tenant, boxIDs, true, true); err != nil {
			return err
		}
		if len(tagIDs) == 0 {
			return tag.Authorize(ctx, tx, tenant, nil, true, true)
		}
		for offset := 0; offset < len(tagIDs); offset += 25 {
			if err := tag.Authorize(ctx, tx, tenant, tagIDs[offset:min(offset+25, len(tagIDs))], true, true); err != nil {
				return err
			}
		}
		return nil
	}, nil
}
