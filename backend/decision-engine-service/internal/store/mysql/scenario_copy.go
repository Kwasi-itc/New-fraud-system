package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// ScenarioCopyInput supplies a stable destination identity for reconciliation.
// Copy is deliberately not auto-retried after an uncertain commit; inspect the
// destination first. It copies a consistent, bounded authoring snapshot, NOT
// decisions, snoozes, jobs, publication history or other services' records.
// As in the source copy operation, existing rule snooze-group references are
// retained; this does not clone or reset the referenced snooze state.
type ScenarioCopyInput struct {
	SourceID, DestinationID uuid.UUID
	Name                    string
}

type ScenarioCopySnapshot struct {
	Scenario   scenario.Scenario
	Iterations []scenario.Iteration
	Rules      []scenario.Rule
	Workflows  []workflow.StructuredRule
}

// ScenarioCopyGuard validates current model/dependency permissions using local
// owner contracts on the destination transaction before writes. The source is a
// coherent earlier snapshot; this callback must not pretend it is current data.
// Workflow routing/tag references need their real owners' authorization; generic
// scenario author permission does not grant case access. No network/DDL/commit.
type ScenarioCopyGuard func(context.Context, *sql.Tx, ScenarioCopySnapshot) error

func (s *AuthoringStore) copySnapshot(ctx context.Context, source uuid.UUID) (ScenarioCopySnapshot, error) {
	if s == nil || s.configuration == nil || source == uuid.Nil {
		return ScenarioCopySnapshot{}, ErrTenantScope
	}
	c := s.configuration
	out := ScenarioCopySnapshot{}
	err := mysqlstore.Snapshot(ctx, c.db, func(tx *sql.Tx) error {
		if err := s.authorize(ctx, tx, c.tenant, AuthoringRead); err != nil {
			return err
		}
		if err := s.authorize(ctx, tx, c.tenant, AuthoringWorkflowRead); err != nil {
			return err
		}
		r := authoringRepository{platformRepository: platformRepository{tx, c.schema, c.tenant, c.maxRows, c.maxBytes, false}}
		tenant, parent := c.tenant.String(), source.String()
		var err error
		out.Scenario, err = (ScenarioRepository{r}).GetByID(ctx, tenant, parent)
		if err != nil {
			return err
		}
		out.Iterations, err = (ScenarioIterationRepository{r}).ListByScenario(ctx, tenant, parent)
		if err != nil {
			return err
		}
		// One indexed scenario join, not a rules query per iteration.
		rules := RuleRepository{r}
		out.Rules, err = platformList(ctx, r.platformRepository, rules.selectSQL()+" WHERE r.tenant_id=? AND i.scenario_id=? ORDER BY r.iteration_id,r.display_order,r.created_at,r.id", []any{mysqlstore.UUID(c.tenant), mysqlstore.UUID(source)}, rules.scan)
		if err != nil {
			return err
		}
		out.Workflows, err = (WorkflowRuleRepository{r}).ListStructured(ctx, tenant, parent)
		if err != nil {
			return err
		}
		return copySnapshotBudget(out, c.maxRows, c.maxBytes)
	})
	if err != nil {
		return ScenarioCopySnapshot{}, err
	}
	return out, nil
}

func copySnapshotBudget(v ScenarioCopySnapshot, maxRows, maxBytes int) error {
	count := len(v.Iterations) + len(v.Rules) + len(v.Workflows)
	for _, w := range v.Workflows {
		count += len(w.Conditions) + len(w.Actions)
	}
	if count > maxRows {
		return ErrProjectionLimit
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(raw) > maxBytes {
		return ErrProjectionLimit
	}
	return nil
}

// reidentifyCopy is pure and deep-copies JSON/pointer fields through the bounded
// snapshot encoding. Source identities never leak into destination relationships.
// Source committed statuses/versions and its live selection are preserved, but
// the actual live link is installed only after a new publication guard/event.
func reidentifyCopy(source ScenarioCopySnapshot, input ScenarioCopyInput, at time.Time) (ScenarioCopySnapshot, error) {
	if input.SourceID == uuid.Nil || input.DestinationID == uuid.Nil || input.SourceID == input.DestinationID || source.Scenario.ID != input.SourceID.String() {
		return ScenarioCopySnapshot{}, ErrTenantScope
	}
	raw, err := json.Marshal(source)
	if err != nil {
		return ScenarioCopySnapshot{}, err
	}
	var out ScenarioCopySnapshot
	if err := json.Unmarshal(raw, &out); err != nil {
		return ScenarioCopySnapshot{}, err
	}
	tenant := source.Scenario.TenantID
	if id, err := uuid.Parse(tenant); err != nil || id == uuid.Nil {
		return ScenarioCopySnapshot{}, ErrTenantScope
	}
	out.Scenario.ID, out.Scenario.Name = input.DestinationID.String(), input.Name
	if out.Scenario.Name == "" {
		out.Scenario.Name = source.Scenario.Name + " Copy"
	}
	out.Scenario.CreatedAt, out.Scenario.UpdatedAt, out.Scenario.LiveIterationID = at, at, nil
	iterations := map[string]string{}
	for i := range out.Iterations {
		v := &out.Iterations[i]
		if v.TenantID != tenant || v.ScenarioID != source.Scenario.ID || iterations[v.ID] != "" {
			return ScenarioCopySnapshot{}, ErrTenantScope
		}
		old := v.ID
		v.ID, v.ScenarioID, v.CreatedAt = uuid.NewString(), out.Scenario.ID, at
		iterations[old] = v.ID
		if v.Status == scenario.IterationStatusCommitted {
			value := at
			v.CommittedAt = &value
		}
		if source.Scenario.LiveIterationID != nil && *source.Scenario.LiveIterationID == old {
			value := v.ID
			out.Scenario.LiveIterationID = &value
		}
	}
	if source.Scenario.LiveIterationID != nil && out.Scenario.LiveIterationID == nil {
		return ScenarioCopySnapshot{}, errors.New("source live iteration missing from bounded copy snapshot")
	}
	ruleIDs := orderedCopyIDs(len(out.Rules))
	for i := range out.Rules {
		v := &out.Rules[i]
		parent, ok := iterations[v.IterationID]
		if !ok || v.TenantID != tenant {
			return ScenarioCopySnapshot{}, ErrTenantScope
		}
		v.ID, v.IterationID, v.StableRuleID = ruleIDs[i], parent, uuid.NewString()
		v.CreatedAt, v.UpdatedAt = at, at
	}
	workflowIDs := orderedCopyIDs(len(out.Workflows))
	for i := range out.Workflows {
		v := &out.Workflows[i]
		if v.Rule.TenantID != tenant || v.Rule.ScenarioID != source.Scenario.ID {
			return ScenarioCopySnapshot{}, ErrTenantScope
		}
		old := v.Rule.ID
		v.Rule.ID, v.Rule.ScenarioID = workflowIDs[i], out.Scenario.ID
		v.Rule.CreatedAt, v.Rule.UpdatedAt = at, at
		conditionIDs, actionIDs := orderedCopyIDs(len(v.Conditions)), orderedCopyIDs(len(v.Actions))
		for j := range v.Conditions {
			child := &v.Conditions[j]
			if child.TenantID != tenant || child.RuleID != old {
				return ScenarioCopySnapshot{}, ErrTenantScope
			}
			child.ID, child.RuleID, child.CreatedAt, child.UpdatedAt = conditionIDs[j], v.Rule.ID, at, at
		}
		for j := range v.Actions {
			child := &v.Actions[j]
			if child.TenantID != tenant || child.RuleID != old {
				return ScenarioCopySnapshot{}, ErrTenantScope
			}
			child.ID, child.RuleID, child.CreatedAt, child.UpdatedAt = actionIDs[j], v.Rule.ID, at, at
		}
	}
	return out, nil
}

// CopyScenario preserves source Copy scope (iterations/rules and non-deleted
// structured workflows). Legacy workflow definitions and screening/scoring
// configurations are NOT copied by the source either. Snapshot acquisition is
// read-only; no source/database lock is held across network calls or the copy.
func (s *AuthoringStore) CopyScenario(ctx context.Context, input ScenarioCopyInput, dependencies ScenarioCopyGuard, publish PublicationGuard) (scenario.Scenario, error) {
	if dependencies == nil || publish == nil {
		return scenario.Scenario{}, errors.New("dependency and publication preparation guards required")
	}
	if input.SourceID == uuid.Nil || input.DestinationID == uuid.Nil || input.SourceID == input.DestinationID {
		return scenario.Scenario{}, ErrTenantScope
	}
	snapshot, err := s.copySnapshot(ctx, input.SourceID)
	if err != nil {
		return scenario.Scenario{}, err
	}
	c := s.configuration
	var result scenario.Scenario
	err = mysqlstore.Mutation(ctx, c.db, func(tx *sql.Tx) error {
		for _, action := range []AuthoringAction{AuthoringWrite, AuthoringWorkflowWrite} {
			if err := s.authorize(ctx, tx, c.tenant, action); err != nil {
				return err
			}
		}
		if snapshot.Scenario.LiveIterationID != nil {
			if err := s.authorize(ctx, tx, c.tenant, AuthoringPublish); err != nil {
				return err
			}
		}
		if err := dependencies(ctx, tx, snapshot); err != nil {
			return err
		}
		var now mysqlstore.DateTime
		if err := tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&now); err != nil {
			return err
		}
		copy, err := reidentifyCopy(snapshot, input, now.Time)
		if err != nil {
			return err
		}
		r := authoringRepository{platformRepository: platformRepository{tx, c.schema, c.tenant, c.maxRows, c.maxBytes, true}, publication: true, workflowGuards: make(map[uuid.UUID]bool)}
		parent := copy.Scenario
		parent.LiveIterationID = nil
		created, err := (ScenarioRepository{r}).Create(ctx, parent)
		if err != nil {
			return err
		}
		grouped := map[string][]scenario.Rule{}
		for _, rule := range copy.Rules {
			grouped[rule.IterationID] = append(grouped[rule.IterationID], rule)
		}
		sort.Slice(copy.Iterations, func(i, j int) bool { return copy.Iterations[i].Version < copy.Iterations[j].Version })
		for _, iteration := range copy.Iterations {
			status := iteration.Status
			iteration.Status, iteration.CommittedAt = scenario.IterationStatusDraft, nil
			if _, err := (ScenarioIterationRepository{r}).Create(ctx, iteration); err != nil {
				return err
			}
			if _, err := (RuleRepository{r}).CreateMany(ctx, c.tenant.String(), parent.ID, iteration.ID, grouped[iteration.ID]); err != nil {
				return err
			}
			if status == scenario.IterationStatusCommitted {
				if _, err := (ScenarioIterationRepository{r}).Commit(ctx, c.tenant.String(), parent.ID, iteration.ID, now.Time); err != nil {
					return err
				}
			} else if status != scenario.IterationStatusDraft {
				return errors.New("unsupported copied iteration status")
			}
		}
		if err := writeCopiedWorkflows(ctx, r, copy.Workflows); err != nil {
			return err
		}
		if live := copy.Scenario.LiveIterationID; live != nil {
			iteration, err := (ScenarioIterationRepository{r}).GetByID(ctx, c.tenant.String(), parent.ID, *live)
			if err != nil {
				return err
			}
			if err := publish(ctx, tx, PublicationSnapshot{created, iteration, grouped[*live]}); err != nil {
				return err
			}
			if err := (ScenarioRepository{r}).SetLiveIterationID(ctx, c.tenant.String(), parent.ID, live); err != nil {
				return err
			}
			if _, err := (ScenarioPublicationRepository{r}).Create(ctx, scenario.Publication{ID: uuid.NewString(), TenantID: c.tenant.String(), ScenarioID: parent.ID, IterationID: *live, Action: scenario.PublicationActionPublish, CreatedAt: now.Time}); err != nil {
				return err
			}
			created.LiveIterationID = live
		}
		result = created
		return nil
	})
	if err != nil {
		return scenario.Scenario{}, err
	}
	return result, nil
}

// Batched source assembly is shared by scenario and standalone workflow copy.
// Writes reuse the owner's mutation methods and exact authoring guard; they
// never write another service's tables or copy historical/tombstoned identities.
func writeCopiedWorkflows(ctx context.Context, r authoringRepository, values []workflow.StructuredRule) error {
	for _, v := range values {
		if _, err := (WorkflowRuleRepository{r}).Create(ctx, v.Rule); err != nil {
			return err
		}
		for _, child := range v.Conditions {
			if _, err := (WorkflowConditionRepository{workflowChildRepository{r, false}}).Create(ctx, child); err != nil {
				return err
			}
		}
		for _, child := range v.Actions {
			if _, err := (WorkflowActionRepository{workflowChildRepository{r, true}}).Create(ctx, child); err != nil {
				return err
			}
		}
	}
	return nil
}

// Copy timestamps are equal. Assign random identities in byte order so source
// ordering survives created_at/id tie breaks without inventing future times.
func orderedCopyIDs(count int) []string {
	ids := make([]string, count)
	for i := range ids {
		ids[i] = uuid.NewString()
	}
	sort.Strings(ids)
	return ids
}
