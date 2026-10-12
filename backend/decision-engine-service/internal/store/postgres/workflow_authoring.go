package postgres

import (
	"context"
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"reflect"
	"time"
)

type WorkflowAuthoringRepository struct{ DB *pgxpool.Pool }

func authorizeWorkflow(ctx context.Context, q queryable, t string, a workflow.AuthoringActor, write bool) error {
	var allowed bool
	// Existing identity-owner contracts: live workflow changes require platform_admin,
	// and routing additionally requires case access. Identity locks precede domain locks.
	err := q.QueryRow(ctx, `SELECT identity.authorize_data_model_v1($1,$2,$3,$4,$5,$5) AND identity.authorize_case_v1($1,$2,$3,$4,false,$5)`, a.UserID, t, a.SessionID, a.Version, write).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return workflow.ErrAuthoringDenied
	}
	return nil
}
func workflowInbox(ctx context.Context, q queryable, t, inbox string, a workflow.AuthoringActor, lock, requireActive bool) error {
	suffix := ""
	if lock {
		suffix = " FOR SHARE"
	}
	var id string
	if err := q.QueryRow(ctx, `SELECT id FROM case_manager.inboxes WHERE tenant_id=$1 AND id=$2 AND (NOT $3 OR status='active')`+suffix, t, inbox, requireActive).Scan(&id); err != nil {
		return workflow.ErrAuthoringDenied
	}
	var admin bool
	if err := q.QueryRow(ctx, `SELECT identity.authorize_case_v1($1,$2,$3,$4,true,false)`, a.UserID, t, a.SessionID, a.Version).Scan(&admin); err != nil {
		return err
	}
	if admin {
		return nil
	}
	if err := q.QueryRow(ctx, `SELECT user_id FROM case_manager.inbox_users WHERE tenant_id=$1 AND inbox_id=$2 AND user_id=$3`+suffix, t, inbox, a.UserID).Scan(&id); err != nil {
		return workflow.ErrAuthoringDenied
	}
	return nil
}
func decodeCaseRule(v workflow.StructuredRule) (workflow.CaseRule, bool) {
	var out workflow.CaseRule
	if v.Rule.Fallthrough || len(v.Conditions) != 1 || len(v.Actions) != 1 {
		return out, false
	}
	c, a := v.Conditions[0], v.Actions[0]
	var cfg struct {
		Version int      `json:"authoring_version"`
		Inbox   string   `json:"inbox_id"`
		Name    string   `json:"name"`
		Tags    []string `json:"tag_ids"`
	}
	if json.Unmarshal(a.ActionConfig, &cfg) != nil || cfg.Version != 1 || (c.Function != workflow.ConditionNever && c.Function != workflow.ConditionOutcomeIn) {
		return out, false
	}
	if json.Unmarshal(c.Params, &out.Outcomes) != nil {
		return out, false
	}
	out.ID = v.Rule.ID
	out.Name = v.Rule.Name
	out.Enabled = c.Function == workflow.ConditionOutcomeIn
	out.ActionType = a.ActionType
	out.InboxID = cfg.Inbox
	out.Title = cfg.Name
	out.TagIDs = cfg.Tags
	out.UpdatedAt = &v.Rule.UpdatedAt
	return out, out.Validate() == nil
}
func (r WorkflowAuthoringRepository) List(ctx context.Context, t, sc string, a workflow.AuthoringActor) (workflow.AuthoringPage, error) {
	out := workflow.AuthoringPage{Rules: []workflow.CaseRule{}}
	if err := authorizeWorkflow(ctx, r.DB, t, a, false); err != nil {
		return out, err
	}
	if err := r.DB.QueryRow(ctx, `SELECT name,EXISTS(SELECT 1 FROM core.workflows w WHERE w.tenant_id=$1 AND w.scenario_id=$2 AND w.active) FROM core.scenarios WHERE tenant_id=$1 AND id=$2`, t, sc).Scan(&out.ScenarioName, &out.LegacyWorkflows); err != nil {
		return out, err
	}
	values, err := NewWorkflowRuleRepository(r.DB).ListStructured(ctx, t, sc)
	if err != nil {
		return out, err
	}
	// Fetch all authorized inbox IDs once, rather than an authorization query per rule.
	inboxIDs := []string{}
	for _, v := range values {
		if item, ok := decodeCaseRule(v); ok {
			inboxIDs = append(inboxIDs, item.InboxID)
		}
	}
	rows, err := r.DB.Query(ctx, `SELECT i.id FROM case_manager.inboxes i WHERE i.tenant_id=$1 AND i.id=ANY($5::uuid[]) AND (identity.authorize_case_v1($2,$1,$3,$4,true,false) OR EXISTS(SELECT 1 FROM case_manager.inbox_users u WHERE u.tenant_id=i.tenant_id AND u.inbox_id=i.id AND u.user_id=$2))`, t, a.UserID, a.SessionID, a.Version, inboxIDs)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	visible := map[string]bool{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return out, err
		}
		visible[id] = true
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	for _, v := range values {
		item, ok := decodeCaseRule(v)
		if !ok {
			out.OtherRules++
			continue
		}
		if visible[item.InboxID] {
			out.Rules = append(out.Rules, item)
		}
	}
	return out, nil
}
func (r WorkflowAuthoringRepository) Save(ctx context.Context, t, sc string, a workflow.AuthoringActor, in workflow.CaseRule) (workflow.CaseRule, error) {
	empty := workflow.CaseRule{}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(context.Background())
	if err = authorizeWorkflow(ctx, tx, t, a, true); err != nil {
		return empty, err
	}
	// Serialize authoring for this scenario only, not decision evaluation or intake.
	var scenario string
	if err = tx.QueryRow(ctx, `SELECT id FROM core.scenarios WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, t, sc).Scan(&scenario); err != nil {
		return empty, err
	}
	if err = workflowInbox(ctx, tx, t, in.InboxID, a, true, in.Enabled); err != nil {
		return empty, err
	}
	var tags int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM (SELECT id FROM case_manager.tags WHERE tenant_id=$1 AND id=ANY($2::uuid[]) AND (NOT $3 OR deleted_at IS NULL) ORDER BY id FOR SHARE) t`, t, in.TagIDs, in.Enabled).Scan(&tags); err != nil {
		return empty, err
	}
	if tags != len(in.TagIDs) {
		return empty, workflow.ErrAuthoringDenied
	}
	var legacy bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.workflows WHERE tenant_id=$1 AND scenario_id=$2 AND active)`, t, sc).Scan(&legacy); err != nil {
		return empty, err
	}
	if legacy {
		return empty, workflow.ErrAuthoringConflict
	}
	values, err := NewWorkflowRuleRepository(tx).ListStructured(ctx, t, sc)
	if err != nil {
		return empty, err
	}
	var current *workflow.StructuredRule
	priority := 0
	for i := range values {
		if values[i].Rule.ID == in.ID {
			current = &values[i]
		}
		if values[i].Rule.Priority >= priority {
			priority = values[i].Rule.Priority + 1
		}
	}
	if current == nil && (in.UpdatedAt != nil || len(values) >= 500) {
		return empty, workflow.ErrAuthoringConflict
	}
	if current != nil {
		old, ok := decodeCaseRule(*current)
		if !ok {
			return empty, workflow.ErrAuthoringConflict
		}
		if err = workflowInbox(ctx, tx, t, old.InboxID, a, true, false); err != nil {
			return empty, err
		}
		oldVersion := old.UpdatedAt
		old.UpdatedAt = nil
		check := in
		check.UpdatedAt = nil
		if reflect.DeepEqual(old, check) {
			old.UpdatedAt = oldVersion
			return old, tx.Commit(ctx)
		}
		if in.UpdatedAt == nil || !in.UpdatedAt.Equal(*oldVersion) {
			return empty, workflow.ErrAuthoringConflict
		}
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if current != nil && !now.After(current.Rule.UpdatedAt) {
		now = current.Rule.UpdatedAt.Add(time.Microsecond)
	}
	params, _ := json.Marshal(in.Outcomes)
	cfg, _ := json.Marshal(map[string]any{"authoring_version": 1, "inbox_id": in.InboxID, "name": in.Title, "tag_ids": in.TagIDs})
	function := workflow.ConditionNever
	if in.Enabled {
		function = workflow.ConditionOutcomeIn
	}
	if current == nil {
		rule := workflow.Rule{ID: in.ID, TenantID: t, ScenarioID: sc, Name: in.Name, Priority: priority, CreatedAt: now, UpdatedAt: now}
		if _, err = NewWorkflowRuleRepository(tx).Create(ctx, rule); err != nil {
			return empty, err
		}
		if _, err = NewWorkflowConditionRepository(tx).Create(ctx, workflow.Condition{ID: uuid.NewString(), TenantID: t, RuleID: in.ID, Function: function, Params: params, CreatedAt: now, UpdatedAt: now}); err != nil {
			return empty, err
		}
		if _, err = NewWorkflowActionRepository(tx).Create(ctx, workflow.Action{ID: uuid.NewString(), TenantID: t, RuleID: in.ID, ActionType: in.ActionType, ActionConfig: cfg, CreatedAt: now, UpdatedAt: now}); err != nil {
			return empty, err
		}
	} else {
		rule := current.Rule
		rule.Name = in.Name
		rule.UpdatedAt = now
		if _, err = NewWorkflowRuleRepository(tx).Update(ctx, rule); err != nil {
			return empty, err
		}
		c := current.Conditions[0]
		c.Function = function
		c.Params = params
		c.UpdatedAt = now
		if _, err = NewWorkflowConditionRepository(tx).Update(ctx, c); err != nil {
			return empty, err
		}
		act := current.Actions[0]
		act.ActionType = in.ActionType
		act.ActionConfig = cfg
		act.UpdatedAt = now
		if _, err = NewWorkflowActionRepository(tx).Update(ctx, act); err != nil {
			return empty, err
		}
	}
	in.UpdatedAt = &now
	snapshot, _ := json.Marshal(in)
	if _, err = tx.Exec(ctx, `INSERT INTO core.workflow_authoring_events(id,tenant_id,scenario_id,rule_id,actor_id,snapshot,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, uuid.NewString(), t, sc, in.ID, a.UserID, snapshot, now); err != nil {
		return empty, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, err
	}
	return in, nil
}
