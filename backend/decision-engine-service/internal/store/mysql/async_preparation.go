package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

// PreparationForAsync selects live versions once per attempt, unlike scheduled
// execution which uses the owner's persisted iteration. Immutable iterations
// and rules are loaded in bounded batches, never one query per scenario. This
// is the source evaluation consistency contract, not a global data snapshot.
func (s *ExecutionStore) PreparationForAsync(ctx context.Context, identity mysqljobs.Identity) (AsyncExecutionRecord, []ExecutionPreparationSnapshot, error) {
	if s == nil || s.configuration == nil || s.authorize == nil || identity.Validate() != nil || identity.Kind != ports.JobAsync || identity.TenantID != s.configuration.tenant {
		return AsyncExecutionRecord{}, nil, ErrExecutionLease
	}
	c := s.configuration
	var owner AsyncExecutionRecord
	var snapshots []ExecutionPreparationSnapshot
	err := mysqlstore.Mutation(ctx, c.db, func(tx *sql.Tx) error {
		if err := s.authorize(ctx, tx, c.tenant, ExecutionRun); err != nil {
			return err
		}
		var err error
		owner, err = s.repositories(tx, ExecutionRun, nil).Async.GetByID(ctx, c.tenant.String(), identity.ResourceID.String())
		if err != nil {
			return err
		}
		if owner.Status != execution.StatusRunning || owner.Generation != identity.RedriveGeneration || owner.AttemptCount != identity.DomainAttempt {
			return ErrExecutionState
		}
		r := authoringRepository{platformRepository: platformRepository{tx, c.schema, c.tenant, c.maxRows, c.maxBytes, false}}
		parents := []scenario.Scenario{}
		if owner.ScenarioID != "" {
			parent, err := (ScenarioRepository{r}).GetByID(ctx, owner.TenantID, owner.ScenarioID)
			if err != nil {
				return err
			}
			parents = append(parents, parent)
		} else {
			parents, err = (ScenarioRepository{r}).ListLiveByTriggerObject(ctx, owner.TenantID, owner.ObjectType)
			if err != nil {
				return err
			}
		}
		if len(parents) == 0 {
			snapshots = []ExecutionPreparationSnapshot{}
			return nil
		}
		marks, args := []string{}, []any{mysqlstore.UUID(c.tenant)}
		index := map[string]int{}
		for i, parent := range parents {
			if parent.TriggerObjectType != owner.ObjectType || parent.LiveIterationID == nil {
				return ErrExecutionState
			}
			index[*parent.LiveIterationID] = i
			marks = append(marks, "?")
			ids, err := r.ids(owner.TenantID, *parent.LiveIterationID)
			if err != nil {
				return err
			}
			args = append(args, ids[1])
			snapshots = append(snapshots, ExecutionPreparationSnapshot{parent: parent, rules: []scenario.Rule{}})
		}
		iterationsRepo := ScenarioIterationRepository{r}
		iterations, err := platformList(ctx, r.platformRepository, iterationsRepo.selectSQL()+" WHERE tenant_id=? AND id IN("+strings.Join(marks, ",")+") ORDER BY id", args, iterationsRepo.scan)
		if err != nil {
			return err
		}
		if len(iterations) != len(parents) {
			return ErrOwnerRelationshipConflict
		}
		for _, iteration := range iterations {
			i, ok := index[iteration.ID]
			if !ok || iteration.ScenarioID != parents[i].ID || iteration.Status != scenario.IterationStatusCommitted || iteration.CommittedAt == nil {
				return ErrOwnerRelationshipConflict
			}
			snapshots[i].iteration = iteration
		}
		rulesRepo := RuleRepository{r}
		rules, err := platformList(ctx, r.platformRepository, rulesRepo.selectSQL()+" WHERE r.tenant_id=? AND r.iteration_id IN("+strings.Join(marks, ",")+") ORDER BY r.iteration_id,r.display_order,r.created_at,r.id", args, rulesRepo.scan)
		if err != nil {
			return err
		}
		for _, rule := range rules {
			i, ok := index[rule.IterationID]
			if !ok {
				return ErrOwnerRelationshipConflict
			}
			snapshots[i].rules = append(snapshots[i].rules, rule)
		}
		// Bound the combined projection, not just each query/table separately.
		used := len(owner.RequestBody)
		for _, snapshot := range snapshots {
			raw, err := json.Marshal(struct {
				Parent    scenario.Scenario
				Iteration scenario.Iteration
				Rules     []scenario.Rule
			}{snapshot.parent, snapshot.iteration, snapshot.rules})
			if err != nil {
				return err
			}
			used += len(raw)
			if used > c.maxBytes {
				return ErrProjectionLimit
			}
		}
		return nil
	})
	if err != nil {
		return AsyncExecutionRecord{}, nil, err
	}
	owner.RequestBody = append([]byte(nil), owner.RequestBody...)
	return owner, snapshots, nil
}

// ValidateExecutionPreparations batches registry gates across every selected
// scenario. It prevents per-rule/scenario registry queries and holds gates only
// during local preflight, never during model/provider/AST work.
func (r TenantDataReader) ValidateExecutionPreparations(ctx context.Context, snapshots []ExecutionPreparationSnapshot, model ports.TenantModel, objectType string) error {
	if len(snapshots) == 0 {
		return r.ValidateExecutionObjectModel(ctx, model, objectType)
	}
	if r.configuration == nil || len(snapshots) > r.configuration.maxRows {
		return ErrProjectionLimit
	}
	queries := []ports.AggregateQuery{}
	used := 0
	for _, snapshot := range snapshots {
		if snapshot.parent.TenantID != r.configuration.tenant.String() || snapshot.parent.TriggerObjectType != objectType || snapshot.iteration.TenantID != snapshot.parent.TenantID || snapshot.iteration.ScenarioID != snapshot.parent.ID {
			return ErrTenantScope
		}
		q, err := aggregatePublicationQueries(ctx, PublicationSnapshot{Scenario: snapshot.parent, Iteration: snapshot.iteration, Rules: snapshot.rules}, model, r.configuration.maxRows, r.configuration.maxBytes)
		if err != nil {
			return &ports.LocalTenantReadError{Cause: err}
		}
		used += len(q)
		if used > r.configuration.maxRows {
			return ErrProjectionLimit
		}
		queries = append(queries, q...)
	}
	return r.validateExecutionQueries(ctx, map[string]bool{objectType: true}, queries, model)
}
