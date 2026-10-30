package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type RuleRepository struct{ authoringRepository }

var _ ports.RuleRepository = RuleRepository{}

func (r RuleRepository) selectSQL() string {
	return "SELECT r.id,r.tenant_id,r.iteration_id,r.display_order,r.score_modifier,r.snooze_group_id,r.created_at,r.updated_at," + r.project([]string{"r.name", "r.description", "r.rule_group", "r.stable_rule_id"}, "r.formula") + " FROM " + r.schema + ".decision_engine_rules r JOIN " + r.schema + ".decision_engine_scenario_iterations i ON i.tenant_id=r.tenant_id AND i.id=r.iteration_id"
}
func (r RuleRepository) scan(row rowScanner) (scenario.Rule, int64, error) {
	var id, tenant, iteration mysqlstore.UUID
	var order, score mysqlstore.Int32
	var snooze mysqlstore.NullUUID
	var created, updated mysqlstore.DateTime
	var name, desc, group, stable mysqlstore.Text
	var formula mysqlstore.JSON
	var size int64
	if err := row.Scan(&id, &tenant, &iteration, &order, &score, &snooze, &created, &updated, &size, &name, &desc, &group, &stable, &formula); err != nil {
		return scenario.Rule{}, 0, err
	}
	v := scenario.Rule{ID: uuid.UUID(id).String(), TenantID: uuid.UUID(tenant).String(), IterationID: uuid.UUID(iteration).String(), DisplayOrder: int(order), ScoreModifier: int(score), SnoozeGroupID: uuidPointer(snooze), CreatedAt: created.Time, UpdatedAt: updated.Time, Name: string(name), Description: string(desc), RuleGroup: string(group), StableRuleID: string(stable), Formula: append([]byte(nil), formula...)}
	return v, size, r.scope(v.TenantID, v.Name, v.Description, v.RuleGroup, v.StableRuleID)
}
func (r RuleRepository) GetByID(ctx context.Context, tenant, parent, iteration, id string) (scenario.Rule, error) {
	args, err := r.ids(tenant, parent, iteration, id)
	if err != nil {
		return scenario.Rule{}, err
	}
	return platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, r.selectSQL()+" WHERE r.tenant_id=? AND i.scenario_id=? AND r.iteration_id=? AND r.id=?", args...), r.scan)
}
func (r RuleRepository) ListByIteration(ctx context.Context, tenant, parent, iteration string) ([]scenario.Rule, error) {
	args, err := r.ids(tenant, parent, iteration)
	if err != nil {
		return nil, err
	}
	return platformList(ctx, r.platformRepository, r.selectSQL()+" WHERE r.tenant_id=? AND i.scenario_id=? AND r.iteration_id=? ORDER BY r.display_order,r.created_at,r.id", args, r.scan)
}
func (r RuleRepository) ListRuleGroupsByScenario(ctx context.Context, tenant, parent string) ([]string, error) {
	args, err := r.ids(tenant, parent)
	if err != nil {
		return nil, err
	}
	query := "SELECT DISTINCT OCTET_LENGTH(r.rule_group),CASE WHEN OCTET_LENGTH(r.rule_group)<=" + strconv.Itoa(r.maxBytes) + " THEN r.rule_group ELSE '' END AS value FROM " + r.schema + ".decision_engine_rules r JOIN " + r.schema + ".decision_engine_scenario_iterations i ON i.tenant_id=r.tenant_id AND i.id=r.iteration_id WHERE r.tenant_id=? AND i.scenario_id=? AND TRIM(r.rule_group)<>'' ORDER BY value"
	return platformList(ctx, r.platformRepository, query, args, func(row rowScanner) (string, int64, error) {
		var size int64
		var v mysqlstore.Text
		err := row.Scan(&size, &v)
		return string(v), size, err
	})
}
func (r RuleRepository) validate(v scenario.Rule) error {
	if err := v.Validate(); err != nil {
		return err
	}
	if err := r.scope(v.TenantID, v.Name, v.Description, v.RuleGroup, v.StableRuleID, string(v.Formula)); err != nil {
		return err
	}
	if len(v.Formula) > r.maxBytes {
		return ErrProjectionLimit
	}
	if _, err := (mysqlstore.JSON(v.Formula)).Value(); err != nil {
		return err
	}
	if _, err := checkedInt(v.DisplayOrder); err != nil {
		return err
	}
	_, err := checkedInt(v.ScoreModifier)
	return err
}
func (r RuleRepository) Create(ctx context.Context, v scenario.Rule) (scenario.Rule, error) {
	if err := r.validate(v); err != nil {
		return scenario.Rule{}, err
	}
	id, err := r.creation(v.TenantID, v.ID, v.CreatedAt)
	if err != nil {
		return scenario.Rule{}, err
	}
	args, err := r.ids(v.TenantID, v.IterationID)
	if err != nil {
		return scenario.Rule{}, err
	}
	snooze, err := nullUUID(v.SnoozeGroupID)
	if err != nil {
		return scenario.Rule{}, err
	}
	if err := r.draftLock(ctx, v.TenantID, v.IterationID); err != nil {
		return scenario.Rule{}, err
	}
	if _, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_rules(id,tenant_id,iteration_id,display_order,name,description,formula,score_modifier,rule_group,snooze_group_id,stable_rule_id,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)", id, args[0], args[1], v.DisplayOrder, mysqlstore.Text(v.Name), mysqlstore.Text(v.Description), mysqlstore.JSON(v.Formula), v.ScoreModifier, mysqlstore.Text(v.RuleGroup), snooze, mysqlstore.Text(v.StableRuleID), mysqlstore.DateTime{Time: v.CreatedAt}, mysqlstore.DateTime{Time: v.UpdatedAt}); err != nil {
		return scenario.Rule{}, err
	}
	return r.byIterationID(ctx, v.TenantID, v.IterationID, v.ID)
}
func (r RuleRepository) byIterationID(ctx context.Context, tenant, iteration, id string) (scenario.Rule, error) {
	args, err := r.ids(tenant, iteration, id)
	if err != nil {
		return scenario.Rule{}, err
	}
	return platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, r.selectSQL()+" WHERE r.tenant_id=? AND r.iteration_id=? AND r.id=?", args...), r.scan)
}
func (r RuleRepository) Update(ctx context.Context, v scenario.Rule) (scenario.Rule, error) {
	if err := r.validate(v); err != nil {
		return scenario.Rule{}, err
	}
	if err := r.draftLock(ctx, v.TenantID, v.IterationID); err != nil {
		return scenario.Rule{}, err
	}
	args, err := r.ids(v.TenantID, v.IterationID, v.ID)
	if err != nil {
		return scenario.Rule{}, err
	}
	snooze, err := nullUUID(v.SnoozeGroupID)
	if err != nil {
		return scenario.Rule{}, err
	}
	var found mysqlstore.UUID
	if err := r.tx.QueryRowContext(ctx, "SELECT id FROM "+r.schema+".decision_engine_rules WHERE tenant_id=? AND iteration_id=? AND id=? FOR UPDATE", args...).Scan(&found); err != nil {
		return scenario.Rule{}, err
	}
	values := []any{v.DisplayOrder, mysqlstore.Text(v.Name), mysqlstore.Text(v.Description), mysqlstore.JSON(v.Formula), v.ScoreModifier, mysqlstore.Text(v.RuleGroup), snooze, mysqlstore.Text(v.StableRuleID), mysqlstore.DateTime{Time: v.UpdatedAt}}
	values = append(values, args...)
	if _, err := r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_rules SET display_order=?,name=?,description=?,formula=?,score_modifier=?,rule_group=?,snooze_group_id=?,stable_rule_id=?,updated_at=? WHERE tenant_id=? AND iteration_id=? AND id=?", values...); err != nil {
		return scenario.Rule{}, err
	}
	return r.byIterationID(ctx, v.TenantID, v.IterationID, v.ID)
}
func (r RuleRepository) Delete(ctx context.Context, tenant, parent, iteration, id string) error {
	if err := r.draftLock(ctx, tenant, iteration); err != nil {
		return err
	}
	args, err := r.ids(tenant, parent, iteration, id)
	if err != nil {
		return err
	}
	result, err := r.tx.ExecContext(ctx, "DELETE r FROM "+r.schema+".decision_engine_rules r JOIN "+r.schema+".decision_engine_scenario_iterations i ON i.tenant_id=r.tenant_id AND i.id=r.iteration_id WHERE r.tenant_id=? AND i.scenario_id=? AND r.iteration_id=? AND r.id=?", args...)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return err
}

// CreateMany validates the complete batch before DML, writes at most 100 rows
// per statement and reads each chunk back in one query on the same transaction.
// The enclosing mutation must return any error to roll back all chunks.
func (r RuleRepository) CreateMany(ctx context.Context, tenant, parent, iteration string, items []scenario.Rule) ([]scenario.Rule, error) {
	if err := r.mutating(); err != nil {
		return nil, err
	}
	args, err := r.ids(tenant, parent, iteration)
	if err != nil {
		return nil, err
	}
	if len(items) > r.maxRows {
		return nil, ErrProjectionLimit
	}
	ids := make([]mysqlstore.UUID, len(items))
	snoozes := make([]mysqlstore.NullUUID, len(items))
	seen := map[mysqlstore.UUID]bool{}
	used := 0
	for i, v := range items {
		if v.TenantID != uuid.UUID(r.tenant).String() || v.IterationID != iteration {
			return nil, ErrTenantScope
		}
		if err := r.validate(v); err != nil {
			return nil, err
		}
		id, err := r.creation(v.TenantID, v.ID, v.CreatedAt)
		if err != nil {
			return nil, err
		}
		ids[i] = id
		if seen[id] {
			return nil, errors.New("duplicate rule identity in batch")
		}
		seen[id] = true
		snoozes[i], err = nullUUID(v.SnoozeGroupID)
		if err != nil {
			return nil, err
		}
		if _, err := (mysqlstore.DateTime{Time: v.UpdatedAt}).Value(); err != nil {
			return nil, err
		}
		used += len(v.Name) + len(v.Description) + len(v.RuleGroup) + len(v.StableRuleID) + len(v.Formula)
		if used > r.maxBytes {
			return nil, ErrProjectionLimit
		}
	}
	var status string
	if err := r.tx.QueryRowContext(ctx, "SELECT status FROM "+r.schema+".decision_engine_scenario_iterations WHERE tenant_id=? AND scenario_id=? AND id=? FOR SHARE", args...).Scan(&status); err != nil {
		return nil, err
	}
	if status != "draft" {
		return nil, ErrCommittedIteration
	}
	out := make([]scenario.Rule, 0, len(items))
	for start := 0; start < len(items); start += 100 {
		end := min(start+100, len(items))
		values := make([]any, 0, (end-start)*13)
		groups := make([]string, 0, end-start)
		where := make([]string, 0, end-start)
		queryArgs := append([]any(nil), args...)
		for i := start; i < end; i++ {
			v := items[i]
			groups = append(groups, "(?,?,?,?,?,?,?,?,?,?,?,?,?)")
			values = append(values, ids[i], args[0], args[2], v.DisplayOrder, mysqlstore.Text(v.Name), mysqlstore.Text(v.Description), mysqlstore.JSON(v.Formula), v.ScoreModifier, mysqlstore.Text(v.RuleGroup), snoozes[i], mysqlstore.Text(v.StableRuleID), mysqlstore.DateTime{Time: v.CreatedAt}, mysqlstore.DateTime{Time: v.UpdatedAt})
			where = append(where, "?")
			queryArgs = append(queryArgs, ids[i])
		}
		if _, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_rules(id,tenant_id,iteration_id,display_order,name,description,formula,score_modifier,rule_group,snooze_group_id,stable_rule_id,created_at,updated_at) VALUES"+strings.Join(groups, ","), values...); err != nil {
			return nil, err
		}
		stored, err := platformList(ctx, r.platformRepository, r.selectSQL()+" WHERE r.tenant_id=? AND i.scenario_id=? AND r.iteration_id=? AND r.id IN("+strings.Join(where, ",")+") ORDER BY r.display_order,r.created_at,r.id", queryArgs, r.scan)
		if err != nil {
			return nil, err
		}
		byID := make(map[string]scenario.Rule, len(stored))
		for _, v := range stored {
			byID[v.ID] = v
		}
		for i := start; i < end; i++ {
			v, ok := byID[uuid.UUID(ids[i]).String()]
			if !ok {
				return nil, errors.New("rule batch readback incomplete")
			}
			out = append(out, v)
		}
	}
	return out, nil
}
