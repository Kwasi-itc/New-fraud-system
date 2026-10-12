package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
)

// Read rule metadata, conditions and actions in one MVCC snapshot. Indexed lateral
// aggregates avoid a cross product and per-rule round trips.
func (r WorkflowRuleRepository) ListStructured(ctx context.Context, tenant, scenario string) ([]workflow.StructuredRule, error) {
	rows, err := r.q.Query(ctx, `SELECT r.id,r.tenant_id,r.scenario_id,r.name,r.priority,r.fallthrough,r.created_at,r.updated_at,
 coalesce((SELECT jsonb_agg(jsonb_build_object('ID',c.id,'TenantID',c.tenant_id,'RuleID',c.rule_id,'Function',c.function,'Params',c.params) ORDER BY c.created_at,c.id) FROM core.workflow_conditions c WHERE c.tenant_id=r.tenant_id AND c.rule_id=r.id),'[]'),
 coalesce((SELECT jsonb_agg(jsonb_build_object('ID',a.id,'TenantID',a.tenant_id,'RuleID',a.rule_id,'ActionType',a.action_type,'ActionConfig',a.action_config) ORDER BY a.created_at,a.id) FROM core.workflow_actions a WHERE a.tenant_id=r.tenant_id AND a.rule_id=r.id),'[]')
 FROM core.workflow_rules r WHERE r.tenant_id=$1 AND r.scenario_id=$2 ORDER BY r.priority,r.created_at,r.id LIMIT 501`, tenant, scenario)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []workflow.StructuredRule{}
	for rows.Next() {
		var v workflow.StructuredRule
		var c, a []byte
		if err = rows.Scan(&v.Rule.ID, &v.Rule.TenantID, &v.Rule.ScenarioID, &v.Rule.Name, &v.Rule.Priority, &v.Rule.Fallthrough, &v.Rule.CreatedAt, &v.Rule.UpdatedAt, &c, &a); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(c, &v.Conditions); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(a, &v.Actions); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if len(out) > 500 {
		return nil, fmt.Errorf("scenario exceeds 500 workflow rules")
	}
	return out, rows.Err()
}
