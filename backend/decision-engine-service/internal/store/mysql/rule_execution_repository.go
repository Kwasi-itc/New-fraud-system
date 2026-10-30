package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type RuleExecutionRepository struct{ authoringRepository }

var _ ports.RuleExecutionRepository = RuleExecutionRepository{}

func (r RuleExecutionRepository) selectSQL() string {
	// COALESCE affects the size calculation only: the projected SQL NULL remains
	// NULL and is not conflated with a stored JSON null value.
	size := "OCTET_LENGTH(rule_name)+OCTET_LENGTH(outcome)+COALESCE(OCTET_LENGTH(evaluation),0)"
	return "SELECT id,decision_id,rule_id,score_modifier,created_at," + size + ",CASE WHEN " + size + "<=" + strconv.Itoa(r.maxBytes) + " THEN rule_name ELSE '' END,CASE WHEN " + size + "<=" + strconv.Itoa(r.maxBytes) + " THEN outcome ELSE '' END,CASE WHEN " + size + "<=" + strconv.Itoa(r.maxBytes) + " THEN evaluation ELSE NULL END FROM " + r.schema + ".decision_engine_rule_executions"
}
func (r RuleExecutionRepository) scan(row rowScanner) (decision.RuleExecution, int64, error) {
	var id, parent, rule mysqlstore.UUID
	var score mysqlstore.Int32
	var at mysqlstore.DateTime
	var size int64
	var name, outcome mysqlstore.Text
	var raw []byte
	if err := row.Scan(&id, &parent, &rule, &score, &at, &size, &name, &outcome, &raw); err != nil {
		return decision.RuleExecution{}, 0, err
	}
	v := decision.RuleExecution{ID: uuid.UUID(id).String(), DecisionID: uuid.UUID(parent).String(), RuleID: uuid.UUID(rule).String(), RuleName: string(name), Outcome: string(outcome), ScoreModifier: int(score), CreatedAt: at.Time}
	// Preserve the source distinction: SQL NULL means absent; JSON null decodes
	// to an empty evidence node. CreateMany mirrors the source typed-nil encoding.
	if raw != nil {
		v.Evaluation = &domainast.EvaluationNode{}
		if err := json.Unmarshal(raw, v.Evaluation); err != nil {
			return decision.RuleExecution{}, 0, err
		}
	}
	return v, size, r.scope(r.tenant.String(), v.RuleName, v.Outcome)
}
func (r RuleExecutionRepository) ListByDecision(ctx context.Context, tenant, parent string) ([]decision.RuleExecution, error) {
	args, err := r.ids(tenant, parent)
	if err != nil {
		return nil, err
	}
	return platformList(ctx, r.platformRepository, r.selectSQL()+" WHERE tenant_id=? AND decision_id=? ORDER BY created_at,id", args, r.scan)
}

// evidenceBindings resolves all authoritative parent identities in bounded,
// consistently ordered chunks. Tenant-less public evidence objects cannot
// select another tenant's decisions or forge the evidence iteration binding.
func (r authoringRepository) evidenceBindings(ctx context.Context, table string, parents []string) (map[string]mysqlstore.UUID, error) {
	ordered := append([]string(nil), parents...)
	sort.Strings(ordered)
	result := map[string]mysqlstore.UUID{}
	for start := 0; start < len(ordered); start += 100 {
		end := min(start+100, len(ordered))
		ids := ordered[start:end]
		args := []any{mysqlstore.UUID(r.tenant)}
		marks := make([]string, len(ids))
		for i, id := range ids {
			bound, err := r.ids(r.tenant.String(), id)
			if err != nil {
				return nil, err
			}
			args = append(args, bound[1])
			marks[i] = "?"
		}
		rows, err := r.tx.QueryContext(ctx, "SELECT id,scenario_iteration_id FROM "+r.schema+"."+table+" WHERE tenant_id=? AND id IN ("+strings.Join(marks, ",")+") ORDER BY id FOR SHARE", args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, iteration mysqlstore.UUID
			if err := rows.Scan(&id, &iteration); err != nil {
				rows.Close()
				return nil, err
			}
			result[uuid.UUID(id).String()] = iteration
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	if len(result) != len(ordered) {
		return nil, sql.ErrNoRows
	}
	return result, nil
}
func (r RuleExecutionRepository) CreateMany(ctx context.Context, items []decision.RuleExecution) ([]decision.RuleExecution, error) {
	if err := r.mutating(); err != nil {
		return nil, err
	}
	if len(items) > r.maxRows {
		return nil, ErrProjectionLimit
	}
	values := make([][]any, len(items))
	parents := map[string]bool{}
	seen := map[string]bool{}
	used := 0
	for i, v := range items {
		id, err := r.creation(r.tenant.String(), v.ID, v.CreatedAt, v.RuleName, v.Outcome)
		if err != nil {
			return nil, err
		}
		args, err := r.ids(r.tenant.String(), v.DecisionID, v.RuleID)
		if err != nil {
			return nil, err
		}
		if seen[v.ID] {
			return nil, errors.New("duplicate evidence identity")
		}
		seen[v.ID] = true
		parents[v.DecisionID] = true
		score, err := checkedInt(v.ScoreModifier)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(v.Evaluation)
		if err != nil {
			return nil, err
		}
		size := len(v.RuleName) + len(v.Outcome) + len(raw)
		if size > r.maxBytes-used {
			return nil, ErrProjectionLimit
		}
		used += size
		values[i] = []any{id, args[0], nil, args[1], args[2], mysqlstore.Text(v.RuleName), mysqlstore.Text(v.Outcome), score, mysqlstore.JSON(raw), mysqlstore.DateTime{Time: v.CreatedAt}}
	}
	keys := make([]string, 0, len(parents))
	for key := range parents {
		keys = append(keys, key)
	}
	bindings, err := r.evidenceBindings(ctx, "decision_engine_decisions", keys)
	if err != nil {
		return nil, err
	}
	out := make([]decision.RuleExecution, 0, len(items))
	for start := 0; start < len(items); start += 100 {
		end := min(start+100, len(items))
		args := []any{}
		marks := []string{}
		readArgs := []any{mysqlstore.UUID(r.tenant)}
		readMarks := []string{}
		for i := start; i < end; i++ {
			values[i][2] = bindings[items[i].DecisionID]
			args = append(args, values[i]...)
			marks = append(marks, "(?,?,?,?,?,?,?,?,?,?)")
			readArgs = append(readArgs, values[i][0])
			readMarks = append(readMarks, "?")
		}
		if _, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_rule_executions(id,tenant_id,scenario_iteration_id,decision_id,rule_id,rule_name,outcome,score_modifier,evaluation,created_at) VALUES"+strings.Join(marks, ","), args...); err != nil {
			return nil, err
		}
		stored, err := platformList(ctx, r.platformRepository, r.selectSQL()+" WHERE tenant_id=? AND id IN ("+strings.Join(readMarks, ",")+") ORDER BY id", readArgs, r.scan)
		if err != nil {
			return nil, err
		}
		byID := map[string]decision.RuleExecution{}
		for _, v := range stored {
			byID[v.ID] = v
		}
		for i := start; i < end; i++ {
			v, ok := byID[items[i].ID]
			if !ok {
				return nil, sql.ErrNoRows
			}
			out = append(out, v)
		}
	}
	return out, nil
}
