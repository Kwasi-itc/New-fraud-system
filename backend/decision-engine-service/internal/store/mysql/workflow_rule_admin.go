package mysql

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/google/uuid"
)

func structuredRule(ctx context.Context, r authoringRepository, parent, id uuid.UUID) (workflow.StructuredRule, error) {
	item, err := (WorkflowRuleRepository{r}).GetByID(ctx, r.tenant.String(), parent.String(), id.String())
	if err != nil {
		return workflow.StructuredRule{}, err
	}
	conditions, err := (WorkflowConditionRepository{workflowChildRepository{r, false}}).ListByRule(ctx, r.tenant.String(), id.String())
	if err != nil {
		return workflow.StructuredRule{}, err
	}
	actions, err := (WorkflowActionRepository{workflowChildRepository{r, true}}).ListByRule(ctx, r.tenant.String(), id.String())
	if err != nil {
		return workflow.StructuredRule{}, err
	}
	return workflow.StructuredRule{Rule: item, Conditions: conditions, Actions: actions}, nil
}

func (s *AdministrationStore) WorkflowRules(ctx context.Context, parent uuid.UUID) ([]workflow.StructuredRule, error) {
	return adminRun(ctx, s, false, AuthoringWorkflowRead, func(r authoringRepository) ([]workflow.StructuredRule, error) {
		if _, err := (ScenarioRepository{r}).GetByID(ctx, r.tenant.String(), parent.String()); err != nil {
			return nil, err
		}
		return (WorkflowRuleRepository{r}).ListStructured(ctx, r.tenant.String(), parent.String())
	})
}
func (s *AdministrationStore) WorkflowRule(ctx context.Context, parent, id uuid.UUID) (workflow.StructuredRule, error) {
	return adminRun(ctx, s, false, AuthoringWorkflowRead, func(r authoringRepository) (workflow.StructuredRule, error) {
		return structuredRule(ctx, r, parent, id)
	})
}
func (s *AdministrationStore) SaveWorkflowRule(ctx context.Context, parent, id uuid.UUID, name string, fallthroughEnabled bool) (workflow.StructuredRule, error) {
	return adminRun(ctx, s, true, AuthoringWorkflowWrite, func(r authoringRepository) (workflow.StructuredRule, error) {
		create := id == uuid.Nil
		t := r.tenant.String()
		p := parent.String()
		if err := r.lockWorkflowAuthoring(ctx, t, p); err != nil {
			return workflow.StructuredRule{}, err
		}
		if _, err := (ScenarioRepository{r}).GetByID(ctx, t, p); err != nil {
			return workflow.StructuredRule{}, err
		}
		repo := WorkflowRuleRepository{r}
		var item workflow.Rule
		var err error
		if id == uuid.Nil {
			values, e := repo.ListByScenario(ctx, t, p)
			if e != nil {
				return workflow.StructuredRule{}, e
			}
			if len(values) >= r.maxRows {
				return workflow.StructuredRule{}, ErrProjectionLimit
			}
			priority := 0
			for _, v := range values {
				if v.Priority >= priority {
					priority = v.Priority + 1
				}
			}
			allocated, e := s.identity()
			if e != nil {
				return workflow.StructuredRule{}, e
			}
			id = uuid.MustParse(allocated)
			item = workflow.Rule{ID: allocated, TenantID: t, ScenarioID: p, Priority: priority, CreatedAt: s.now()}
		} else {
			item, err = repo.GetByID(ctx, t, p, id.String())
			if err != nil {
				return workflow.StructuredRule{}, err
			}
		}
		item.Name, item.Fallthrough, item.UpdatedAt = name, fallthroughEnabled, s.now()
		if err := item.Validate(); err != nil {
			return workflow.StructuredRule{}, errors.Join(ErrAdministrationInput, err)
		}
		if create {
			if _, err = repo.Create(ctx, item); err != nil {
				return workflow.StructuredRule{}, err
			}
		} else {
			if _, err = repo.Update(ctx, item); err != nil {
				return workflow.StructuredRule{}, err
			}
		}
		return structuredRule(ctx, r, parent, id)
	})
}
func (s *AdministrationStore) DeleteWorkflowRule(ctx context.Context, parent, id uuid.UUID) error {
	_, err := adminRun(ctx, s, true, AuthoringWorkflowWrite, func(r authoringRepository) (struct{}, error) {
		return struct{}{}, (WorkflowRuleRepository{r}).Delete(ctx, r.tenant.String(), parent.String(), id.String())
	})
	return err
}
func (s *AdministrationStore) ReorderWorkflowRules(ctx context.Context, parent uuid.UUID, ids []string) error {
	_, err := adminRun(ctx, s, true, AuthoringWorkflowWrite, func(r authoringRepository) (struct{}, error) {
		if _, err := (ScenarioRepository{r}).GetByID(ctx, r.tenant.String(), parent.String()); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, (WorkflowRuleRepository{r}).Reorder(ctx, r.tenant.String(), parent.String(), ids, s.now())
	})
	return err
}
func (s *AdministrationStore) SaveWorkflowCondition(ctx context.Context, parent, rule, id uuid.UUID, fields workflow.Condition) (workflow.Condition, error) {
	return adminRun(ctx, s, true, AuthoringWorkflowWrite, func(r authoringRepository) (workflow.Condition, error) {
		if _, err := (WorkflowRuleRepository{r}).GetByID(ctx, r.tenant.String(), parent.String(), rule.String()); err != nil {
			return workflow.Condition{}, err
		}
		repo := WorkflowConditionRepository{workflowChildRepository{r, false}}
		if id == uuid.Nil {
			allocated, err := s.identity()
			if err != nil {
				return workflow.Condition{}, err
			}
			fields.ID, fields.TenantID, fields.RuleID, fields.CreatedAt, fields.UpdatedAt = allocated, r.tenant.String(), rule.String(), s.now(), s.now()
			return repo.Create(ctx, fields)
		}
		item, err := repo.GetByID(ctx, r.tenant.String(), rule.String(), id.String())
		if err != nil {
			return workflow.Condition{}, err
		}
		item.Function, item.Params, item.UpdatedAt = fields.Function, fields.Params, s.now()
		return repo.Update(ctx, item)
	})
}
func (s *AdministrationStore) SaveWorkflowAction(ctx context.Context, parent, rule, id uuid.UUID, fields workflow.Action) (workflow.Action, error) {
	return adminRun(ctx, s, true, AuthoringWorkflowWrite, func(r authoringRepository) (workflow.Action, error) {
		if _, err := (WorkflowRuleRepository{r}).GetByID(ctx, r.tenant.String(), parent.String(), rule.String()); err != nil {
			return workflow.Action{}, err
		}
		if workflow.IsCaseAction(fields.ActionType) {
			if err := s.workflowDestinations(ctx, r, []workflow.Definition{{ActionType: fields.ActionType, ActionConfig: fields.ActionConfig}}, true); err != nil {
				return workflow.Action{}, err
			}
		}
		if fields.ActionType == workflow.ActionTypeAddTag || workflow.IsCaseAction(fields.ActionType) {
			var cfg struct {
				TagIDs []string `json:"tag_ids"`
			}
			if err := json.Unmarshal(fields.ActionConfig, &cfg); err != nil {
				return workflow.Action{}, ErrAdministrationInput
			}
			tags := make([]uuid.UUID, len(cfg.TagIDs))
			for i, id := range cfg.TagIDs {
				var err error
				tags[i], err = uuid.Parse(id)
				if err != nil {
					return workflow.Action{}, ErrAdministrationInput
				}
			}
			if len(tags) > 0 {
				if err := s.tags.Authorize(ctx, r.tx, r.tenant, tags, true, false); err != nil {
					return workflow.Action{}, err
				}
			}
		}
		repo := WorkflowActionRepository{workflowChildRepository{r, true}}
		if id == uuid.Nil {
			allocated, err := s.identity()
			if err != nil {
				return workflow.Action{}, err
			}
			fields.ID, fields.TenantID, fields.RuleID, fields.CreatedAt, fields.UpdatedAt = allocated, r.tenant.String(), rule.String(), s.now(), s.now()
			return repo.Create(ctx, fields)
		}
		item, err := repo.GetByID(ctx, r.tenant.String(), rule.String(), id.String())
		if err != nil {
			return workflow.Action{}, err
		}
		item.ActionType, item.ActionConfig, item.UpdatedAt = fields.ActionType, fields.ActionConfig, s.now()
		return repo.Update(ctx, item)
	})
}
func (s *AdministrationStore) DeleteWorkflowChild(ctx context.Context, parent, rule, id uuid.UUID, action bool) error {
	_, err := adminRun(ctx, s, true, AuthoringWorkflowWrite, func(r authoringRepository) (struct{}, error) {
		if _, err := (WorkflowRuleRepository{r}).GetByID(ctx, r.tenant.String(), parent.String(), rule.String()); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, (workflowChildRepository{r, action}).delete(ctx, r.tenant.String(), rule.String(), id.String())
	})
	return err
}
