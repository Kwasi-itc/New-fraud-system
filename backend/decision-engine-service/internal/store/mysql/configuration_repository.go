package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type configurationKind uint8

const (
	workflowConfiguration configurationKind = iota
	screeningConfiguration
	scoringConfiguration
)

type configurationRepository struct {
	authoringRepository
	kind configurationKind
}
type configurationRecord struct {
	id, tenant, parent          string
	order                       int
	name, description, selector string
	config                      mysqlstore.JSON
	active                      bool
	route                       *string
	created, updated            time.Time
	outcomes                    []string
}

func (r configurationRepository) names() (table, child, selector, document, order, description, route string) {
	switch r.kind {
	case workflowConfiguration:
		return "decision_engine_workflows", "decision_engine_workflow_outcomes", "action_type", "action_config", "display_order", "description", "NULL"
	case screeningConfiguration:
		return "decision_engine_screening_configs", "decision_engine_screening_outcomes", "provider", "config_json", "0", "''", "review_inbox_id"
	default:
		return "decision_engine_scoring_configs", "decision_engine_scoring_outcomes", "ruleset_ref", "config_json", "0", "''", "NULL"
	}
}
func (r configurationRepository) selectSQL() string {
	table, _, selector, document, order, description, route := r.names()
	return "SELECT id,tenant_id,scenario_id," + order + ",active," + route + ",created_at,updated_at," + r.project([]string{"name", description, selector}, document) + " FROM " + r.schema + "." + table
}
func (r configurationRepository) scan(row rowScanner) (configurationRecord, int64, error) {
	var id, tenant, parent mysqlstore.UUID
	var order mysqlstore.Int32
	var active mysqlstore.Bool
	var route mysqlstore.NullUUID
	var created, updated mysqlstore.DateTime
	var size int64
	var name, description, selector mysqlstore.Text
	var document mysqlstore.JSON
	if err := row.Scan(&id, &tenant, &parent, &order, &active, &route, &created, &updated, &size, &name, &description, &selector, &document); err != nil {
		return configurationRecord{}, 0, err
	}
	return configurationRecord{id: uuid.UUID(id).String(), tenant: uuid.UUID(tenant).String(), parent: uuid.UUID(parent).String(), order: int(order), name: string(name), description: string(description), selector: string(selector), config: document, active: bool(active), route: uuidPointer(route), created: created.Time, updated: updated.Time, outcomes: []string{}}, size, nil
}
func (r configurationRepository) get(ctx context.Context, tenant, parent, id string) (configurationRecord, error) {
	args, err := r.ids(tenant, parent, id)
	if err != nil {
		return configurationRecord{}, err
	}
	v, err := platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, r.selectSQL()+" WHERE tenant_id=? AND scenario_id=? AND id=?", args...), r.scan)
	if err != nil {
		return configurationRecord{}, err
	}
	out := []configurationRecord{v}
	if err := r.attachOutcomes(ctx, out); err != nil {
		return configurationRecord{}, err
	}
	return out[0], nil
}
func (r configurationRepository) list(ctx context.Context, tenant, parent string, active bool) ([]configurationRecord, error) {
	args, err := r.ids(tenant, parent)
	if err != nil {
		return nil, err
	}
	where := " WHERE tenant_id=? AND scenario_id=?"
	if active {
		where += " AND active=1"
	}
	order := "created_at DESC,id DESC"
	if r.kind == workflowConfiguration {
		order = "display_order,created_at,id"
	}
	out, err := platformList(ctx, r.platformRepository, r.selectSQL()+where+" ORDER BY "+order, args, r.scan)
	if err != nil {
		return nil, err
	}
	if err := r.attachOutcomes(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// Read all ordered arrays for the already-authorized parent page in bounded
// chunks. Parent and child reads share the caller snapshot, never N+1 queries.
func (r configurationRepository) attachOutcomes(ctx context.Context, records []configurationRecord) error {
	_, child, _, _, _, _, _ := r.names()
	index := map[string]int{}
	used := 0
	nodes := 0
	for i, v := range records {
		index[v.id] = i
		used += len(v.name) + len(v.description) + len(v.selector) + len(v.config)
		if used > r.maxBytes {
			return ErrProjectionLimit
		}
	}
	for start := 0; start < len(records); start += 100 {
		end := min(start+100, len(records))
		args := []any{mysqlstore.UUID(r.tenant)}
		marks := []string{}
		for _, v := range records[start:end] {
			marks = append(marks, "?")
			args = append(args, mysqlstore.UUID(uuid.MustParse(v.id)))
		}
		rows, err := r.tx.QueryContext(ctx, "SELECT config_id,position,COALESCE(OCTET_LENGTH(value),0),CASE WHEN OCTET_LENGTH(value)<=? THEN value ELSE NULL END FROM "+r.schema+"."+child+" WHERE tenant_id=? AND config_id IN ("+strings.Join(marks, ",")+") ORDER BY config_id,position LIMIT ?", append([]any{r.maxBytes}, append(args, r.maxRows+1)...)...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id mysqlstore.UUID
			var position mysqlstore.Int32
			var size int64
			var value sql.NullString
			if err := rows.Scan(&id, &position, &size, &value); err != nil {
				rows.Close()
				return err
			}
			nodes++
			if nodes > r.maxRows || size < 0 || size > int64(r.maxBytes-used) {
				rows.Close()
				return ErrProjectionLimit
			}
			used += int(size)
			i, ok := index[uuid.UUID(id).String()]
			if !ok || int(position) != len(records[i].outcomes) {
				rows.Close()
				return errors.New("invalid ordered outcome identity")
			}
			// The storage contract retains NULL elements. Existing public []string
			// ports cannot represent one; fail explicitly just as source pgx scans do,
			// rather than silently skipping it or replacing it with an empty string.
			if !value.Valid {
				rows.Close()
				return errors.New("NULL outcome is not representable by the public string-array contract")
			}
			records[i].outcomes = append(records[i].outcomes, value.String)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
func (r configurationRepository) validate(v configurationRecord) error {
	if err := r.scope(v.tenant, v.name, v.description, v.selector, string(v.config)); err != nil {
		return err
	}
	if _, err := checkedInt(v.order); err != nil {
		return err
	}
	if _, err := v.config.Value(); err != nil {
		return err
	}
	if v.outcomes == nil {
		return errors.New("allowed outcomes array cannot be SQL NULL")
	}
	if len(v.outcomes) > r.maxRows {
		return ErrProjectionLimit
	}
	used := len(v.name) + len(v.description) + len(v.selector) + len(v.config)
	for _, value := range v.outcomes {
		if _, err := (mysqlstore.Text(value)).Value(); err != nil {
			return err
		}
		if len(value) > r.maxBytes-used {
			return ErrProjectionLimit
		}
		used += len(value)
	}
	_, err := nullUUID(v.route)
	return err
}
func (r configurationRepository) replaceOutcomes(ctx context.Context, id string, values []string) error {
	_, child, _, _, _, _, _ := r.names()
	bound, err := r.ids(r.tenant.String(), id)
	if err != nil {
		return err
	}
	if _, err := r.tx.ExecContext(ctx, "DELETE FROM "+r.schema+"."+child+" WHERE tenant_id=? AND config_id=?", bound...); err != nil {
		return err
	}
	for start := 0; start < len(values); start += 100 {
		end := min(start+100, len(values))
		args := []any{}
		marks := []string{}
		for i := start; i < end; i++ {
			args = append(args, bound[0], bound[1], i, mysqlstore.Text(values[i]))
			marks = append(marks, "(?,?,?,?)")
		}
		if _, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+"."+child+"(tenant_id,config_id,position,value) VALUES"+strings.Join(marks, ","), args...); err != nil {
			return err
		}
	}
	return nil
}
func (r configurationRepository) create(ctx context.Context, v configurationRecord) (configurationRecord, error) {
	if err := r.validate(v); err != nil {
		return configurationRecord{}, err
	}
	if r.kind == workflowConfiguration {
		if err := r.lockWorkflowAuthoring(ctx, v.tenant, v.parent); err != nil {
			return configurationRecord{}, err
		}
	}
	id, err := r.creation(v.tenant, v.id, v.created)
	if err != nil {
		return configurationRecord{}, err
	}
	args, err := r.ids(v.tenant, v.parent)
	if err != nil {
		return configurationRecord{}, err
	}
	table, _, selector, document, _, _, _ := r.names()
	columns := "id,tenant_id,scenario_id,name," + selector + "," + document + ",active,created_at,updated_at"
	marks := "?,?,?,?,?,?,?,?,?"
	values := []any{id, args[0], args[1], mysqlstore.Text(v.name), mysqlstore.Text(v.selector), v.config, mysqlstore.Bool(v.active), mysqlstore.DateTime{Time: v.created}, mysqlstore.DateTime{Time: v.updated}}
	if r.kind == workflowConfiguration {
		columns += ",description,display_order"
		marks += ",?,?"
		values = append(values, mysqlstore.Text(v.description), v.order)
	}
	if r.kind == screeningConfiguration {
		route, err := nullUUID(v.route)
		if err != nil {
			return configurationRecord{}, err
		}
		columns += ",review_inbox_id"
		marks += ",?"
		values = append(values, route)
	}
	if _, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+"."+table+"("+columns+") VALUES("+marks+")", values...); err != nil {
		return configurationRecord{}, err
	}
	if err := r.replaceOutcomes(ctx, v.id, v.outcomes); err != nil {
		return configurationRecord{}, err
	}
	return r.get(ctx, v.tenant, v.parent, v.id)
}
func (r configurationRepository) lock(ctx context.Context, tenant, parent, id string) error {
	if r.kind == workflowConfiguration {
		if err := r.lockWorkflowAuthoring(ctx, tenant, parent); err != nil {
			return err
		}
	}
	if err := r.mutating(); err != nil {
		return err
	}
	args, err := r.ids(tenant, parent, id)
	if err != nil {
		return err
	}
	table, _, _, _, _, _, _ := r.names()
	var found mysqlstore.UUID
	return r.tx.QueryRowContext(ctx, "SELECT id FROM "+r.schema+"."+table+" WHERE tenant_id=? AND scenario_id=? AND id=? FOR UPDATE", args...).Scan(&found)
}
func (r configurationRepository) update(ctx context.Context, v configurationRecord) (configurationRecord, error) {
	if err := r.validate(v); err != nil {
		return configurationRecord{}, err
	}
	if err := r.lock(ctx, v.tenant, v.parent, v.id); err != nil {
		return configurationRecord{}, err
	}
	table, _, selector, document, _, _, _ := r.names()
	set := "name=?," + selector + "=?," + document + "=?,active=?,updated_at=?"
	values := []any{mysqlstore.Text(v.name), mysqlstore.Text(v.selector), v.config, mysqlstore.Bool(v.active), mysqlstore.DateTime{Time: v.updated}}
	if r.kind == workflowConfiguration {
		set += ",description=?"
		values = append(values, mysqlstore.Text(v.description))
	}
	if r.kind == screeningConfiguration {
		route, err := nullUUID(v.route)
		if err != nil {
			return configurationRecord{}, err
		}
		set += ",review_inbox_id=?"
		values = append(values, route)
	}
	args, _ := r.ids(v.tenant, v.parent, v.id)
	if _, err := r.tx.ExecContext(ctx, "UPDATE "+r.schema+"."+table+" SET "+set+" WHERE tenant_id=? AND scenario_id=? AND id=?", append(values, args...)...); err != nil {
		return configurationRecord{}, err
	}
	if err := r.replaceOutcomes(ctx, v.id, v.outcomes); err != nil {
		return configurationRecord{}, err
	}
	return r.get(ctx, v.tenant, v.parent, v.id)
}
func (r configurationRepository) delete(ctx context.Context, tenant, parent, id string) error {
	if err := r.lock(ctx, tenant, parent, id); err != nil {
		return err
	}
	args, _ := r.ids(tenant, parent, id)
	table, _, _, _, _, _, _ := r.names()
	_, err := r.tx.ExecContext(ctx, "DELETE FROM "+r.schema+"."+table+" WHERE tenant_id=? AND scenario_id=? AND id=?", args...)
	return err
}
func (r configurationRepository) reorder(ctx context.Context, tenant, parent string, ids []string, at time.Time) error {
	table, _, _, _, _, _, _ := r.names()
	return r.reorderRows(ctx, tenant, parent, ids, at, table, "display_order", "")
}
