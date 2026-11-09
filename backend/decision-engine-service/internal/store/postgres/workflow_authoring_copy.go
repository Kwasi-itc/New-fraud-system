package postgres

import (
	"context"
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/modelvalidation"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"math"
	"sort"
	"time"
)

// Copy provides the same audited browser graph contract as the staged target.
// Model and case data are read through their shared-schema owner contracts;
// only decision-owned graphs/audit rows are mutated. No network calls occur.
func (r WorkflowAuthoringRepository) Copy(ctx context.Context, tenant, source, destination string, actor workflow.AuthoringActor) ([]workflow.CaseRule, error) {
	for _, id := range []string{tenant, source, destination} {
		v, e := uuid.Parse(id)
		if e != nil || v == uuid.Nil {
			return nil, workflow.ErrAuthoringConflict
		}
	}
	if source == destination {
		return nil, workflow.ErrAuthoringConflict
	}
	tx, e := r.DB.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(context.Background())
	if e = authorizeWorkflow(ctx, tx, tenant, actor, true); e != nil {
		return nil, e
	}
	// Match Save/Delete's per-scenario lock order, sorting both IDs for copies
	// in opposite directions. Unrelated scenarios are never serialized.
	parents, e := tx.Query(ctx, "SELECT id,trigger_object_type FROM core.scenarios WHERE tenant_id=$1 AND id=ANY($2::uuid[]) ORDER BY id FOR UPDATE", tenant, []string{source, destination})
	if e != nil {
		return nil, e
	}
	types := map[string]string{}
	for parents.Next() {
		var id, kind string
		if e = parents.Scan(&id, &kind); e != nil {
			parents.Close()
			return nil, e
		}
		types[id] = kind
	}
	e = parents.Err()
	parents.Close()
	if e != nil {
		return nil, e
	}
	if len(types) != 2 {
		return nil, pgx.ErrNoRows
	}
	graphs, e := NewWorkflowRuleRepository(tx).ListStructured(ctx, tenant, source)
	if e != nil {
		return nil, e
	}
	encoded, e := json.Marshal(graphs)
	if e != nil {
		return nil, e
	}
	if len(encoded) > 2<<20 {
		return nil, workflow.ErrAuthoringConflict
	}
	rules := make([]workflow.CaseRule, len(graphs))
	inboxes, tags := map[string]bool{}, map[string]bool{}
	for i, g := range graphs {
		v, ok := workflow.DecodeCaseRule(g)
		if !ok {
			return nil, workflow.ErrAuthoringConflict
		}
		if e = v.Validate(); e != nil {
			return nil, e
		}
		rules[i] = v
		if v.ActionType != workflow.ActionTypeDoNothing {
			inboxes[v.InboxID] = true
		}
		for _, id := range v.TagIDs {
			tags[id] = true
		}
	}
	ordered := func(values map[string]bool) []string {
		ids := make([]string, 0, len(values))
		for id := range values {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		return ids
	}
	inboxIDs, tagIDs := ordered(inboxes), ordered(tags)
	var admin bool
	if e = tx.QueryRow(ctx, "SELECT identity.authorize_case_v1($1,$2,$3,$4,true,false)", actor.UserID, tenant, actor.SessionID, actor.Version).Scan(&admin); e != nil {
		return nil, e
	}
	var count int
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM (SELECT id FROM case_manager.inboxes WHERE tenant_id=$1 AND id=ANY($2::uuid[]) AND status='active' ORDER BY id FOR SHARE) allowed", tenant, inboxIDs).Scan(&count); e != nil {
		return nil, e
	}
	if count != len(inboxIDs) {
		return nil, workflow.ErrAuthoringDenied
	}
	if !admin {
		if e = tx.QueryRow(ctx, "SELECT count(*) FROM (SELECT inbox_id FROM case_manager.inbox_users WHERE tenant_id=$1 AND inbox_id=ANY($2::uuid[]) AND user_id=$3 ORDER BY inbox_id FOR SHARE) allowed", tenant, inboxIDs, actor.UserID).Scan(&count); e != nil {
			return nil, e
		}
		if count != len(inboxIDs) {
			return nil, workflow.ErrAuthoringDenied
		}
	}
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM (SELECT id FROM case_manager.tags WHERE tenant_id=$1 AND id=ANY($2::uuid[]) AND deleted_at IS NULL ORDER BY id FOR SHARE) allowed", tenant, tagIDs).Scan(&count); e != nil {
		return nil, e
	}
	if count != len(tagIDs) {
		return nil, workflow.ErrAuthoringDenied
	}
	var tableID string
	if e = tx.QueryRow(ctx, "SELECT id FROM core.model_tables WHERE tenant_id=$1 AND name=$2 AND NOT archived FOR SHARE", tenant, types[destination]).Scan(&tableID); e != nil {
		return nil, e
	}
	fields, e := tx.Query(ctx, "SELECT name,data_type,archived FROM core.model_fields WHERE tenant_id=$1 AND table_id=$2 ORDER BY id LIMIT 501 FOR SHARE", tenant, tableID)
	if e != nil {
		return nil, e
	}
	table := ports.TenantModelTable{Name: types[destination], Fields: map[string]ports.TenantModelField{}}
	for fields.Next() {
		var f ports.TenantModelField
		if e = fields.Scan(&f.Name, &f.Type, &f.Archived); e != nil {
			fields.Close()
			return nil, e
		}
		table.Fields[f.Name] = f
	}
	e = fields.Err()
	fields.Close()
	if e != nil {
		return nil, e
	}
	if len(table.Fields) > 500 {
		return nil, workflow.ErrAuthoringConflict
	}
	for _, rule := range rules {
		if rule.Condition != nil {
			if e = modelvalidation.ValidateBrowserCopyCondition(*rule.Condition, table); e != nil {
				return nil, e
			}
		}
	}
	var legacy bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM core.workflows WHERE tenant_id=$1 AND scenario_id=$2 AND active)", tenant, destination).Scan(&legacy); e != nil {
		return nil, e
	}
	if legacy {
		return nil, workflow.ErrAuthoringConflict
	}
	var maximum *int
	if e = tx.QueryRow(ctx, "SELECT count(*),max(priority) FROM (SELECT priority FROM core.workflow_rules WHERE tenant_id=$1 AND scenario_id=$2 AND deleted_at IS NULL LIMIT 501) bounded", tenant, destination).Scan(&count, &maximum); e != nil {
		return nil, e
	}
	if count+len(graphs) > 500 || maximum != nil && *maximum > math.MaxInt32-len(graphs) {
		return nil, workflow.ErrAuthoringConflict
	}
	base := 0
	if maximum != nil && *maximum >= 0 {
		base = *maximum + 1
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	out := make([]workflow.CaseRule, len(graphs))
	for i, g := range graphs {
		g.Rule.ID = uuid.NewString()
		g.Rule.TenantID = tenant
		g.Rule.ScenarioID = destination
		g.Rule.Priority = base + i
		g.Rule.CreatedAt = at
		g.Rule.UpdatedAt = at
		if _, e = NewWorkflowRuleRepository(tx).Create(ctx, g.Rule); e != nil {
			return nil, e
		}
		for j := range g.Conditions {
			c := &g.Conditions[j]
			c.ID = uuid.NewString()
			c.RuleID = g.Rule.ID
			c.CreatedAt = at
			c.UpdatedAt = at
			if _, e = NewWorkflowConditionRepository(tx).Create(ctx, *c); e != nil {
				return nil, e
			}
		}
		for j := range g.Actions {
			a := &g.Actions[j]
			a.ID = uuid.NewString()
			a.RuleID = g.Rule.ID
			a.CreatedAt = at
			a.UpdatedAt = at
			if _, e = NewWorkflowActionRepository(tx).Create(ctx, *a); e != nil {
				return nil, e
			}
		}
		v, ok := workflow.DecodeCaseRule(g)
		if !ok {
			return nil, workflow.ErrAuthoringConflict
		}
		out[i] = v
		snapshot, e := json.Marshal(map[string]any{"operation": "copy", "source_scenario_id": source, "rule": v})
		if e != nil {
			return nil, e
		}
		if _, e = tx.Exec(ctx, "INSERT INTO core.workflow_authoring_events(id,tenant_id,scenario_id,rule_id,actor_id,snapshot,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)", uuid.NewString(), tenant, destination, g.Rule.ID, actor.UserID, snapshot, at); e != nil {
			return nil, e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return out, nil
}
