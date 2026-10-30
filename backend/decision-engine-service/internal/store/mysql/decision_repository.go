package mysql

import (
	"context"
	"errors"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type DecisionRepository struct{ authoringRepository }

var _ ports.DecisionRepository = DecisionRepository{}

func (r DecisionRepository) selectSQL(detail bool) string {
	jsonColumns := []string{}
	if detail {
		jsonColumns = append(jsonColumns, "d.request_body")
	}
	return "SELECT d.id,d.tenant_id,d.scenario_id,d.scenario_iteration_id,d.score,d.triggered,d.created_at," + r.project([]string{"d.object_id", "d.object_type", "d.outcome"}, jsonColumns...) + " FROM " + r.schema + ".decision_engine_decisions d"
}
func (r DecisionRepository) scan(detail bool) func(rowScanner) (decision.Decision, int64, error) {
	return func(row rowScanner) (decision.Decision, int64, error) {
		var id, tenant, parent, iteration mysqlstore.UUID
		var score mysqlstore.Int32
		var triggered mysqlstore.Bool
		var at mysqlstore.DateTime
		var size int64
		var object, kind, outcome mysqlstore.Text
		var request mysqlstore.JSON
		dest := []any{&id, &tenant, &parent, &iteration, &score, &triggered, &at, &size, &object, &kind, &outcome}
		if detail {
			dest = append(dest, &request)
		}
		if err := row.Scan(dest...); err != nil {
			return decision.Decision{}, 0, err
		}
		v := decision.Decision{ID: uuid.UUID(id).String(), TenantID: uuid.UUID(tenant).String(), ScenarioID: uuid.UUID(parent).String(), ScenarioIterationID: uuid.UUID(iteration).String(), ObjectID: string(object), ObjectType: string(kind), Outcome: decision.Outcome(outcome), Score: int(score), Triggered: bool(triggered), CreatedAt: at.Time, RequestBody: append([]byte(nil), request...)}
		return v, size, r.scope(v.TenantID, v.ObjectID, v.ObjectType, string(v.Outcome))
	}
}
func (r DecisionRepository) Create(ctx context.Context, v decision.Decision) (decision.Decision, error) {
	id, err := r.creation(v.TenantID, v.ID, v.CreatedAt, v.ObjectID, v.ObjectType, string(v.Outcome), string(v.RequestBody))
	if err != nil {
		return decision.Decision{}, err
	}
	args, err := r.ids(v.TenantID, v.ScenarioID, v.ScenarioIterationID)
	if err != nil {
		return decision.Decision{}, err
	}
	if _, err = checkedInt(v.Score); err != nil {
		return decision.Decision{}, err
	}
	if _, err = (mysqlstore.JSON(v.RequestBody)).Value(); err != nil {
		return decision.Decision{}, err
	}
	values := []any{id, args[0], args[1], args[2], mysqlstore.Text(v.ObjectID), mysqlstore.Text(v.ObjectType), mysqlstore.JSON(v.RequestBody), mysqlstore.Text(v.Outcome), v.Score, mysqlstore.Bool(v.Triggered), mysqlstore.DateTime{Time: v.CreatedAt}}
	if _, err = r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_decisions(id,tenant_id,scenario_id,scenario_iteration_id,object_id,object_type,request_body,outcome,score,triggered,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)", values...); err != nil {
		return decision.Decision{}, err
	}
	return r.GetByID(ctx, v.TenantID, v.ID)
}
func (r DecisionRepository) GetByID(ctx context.Context, tenant, id string) (decision.Decision, error) {
	args, err := r.ids(tenant, id)
	if err != nil {
		return decision.Decision{}, err
	}
	return platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, r.selectSQL(true)+" WHERE d.tenant_id=? AND d.id=?", args...), r.scan(true))
}

// Values are always parameters. Digest predicates accelerate exact text lookup;
// the complete binary residual remains mandatory even for a digest collision.
func (r DecisionRepository) filter(tenant string, f ports.DecisionListFilter, exactObject ...bool) (string, []any, error) {
	args, err := r.ids(tenant)
	if err != nil {
		return "", nil, err
	}
	if err := r.scope(tenant, f.ObjectIDPrefix, f.ObjectType, f.ObjectID, f.Outcome, f.Search); err != nil {
		return "", nil, err
	}
	clauses := []string{"d.tenant_id=?"}
	for _, p := range []struct{ column, value string }{{"d.id", f.DecisionID}, {"d.scenario_id", f.ScenarioID}} {
		if p.value != "" {
			ids, err := r.ids(tenant, p.value)
			if err != nil {
				return "", nil, err
			}
			clauses = append(clauses, p.column+"=?")
			args = append(args, ids[1])
		}
	}
	for _, p := range []struct{ column, value string }{{"object_type", f.ObjectType}, {"object_id", f.ObjectID}} {
		if p.value != "" || (len(exactObject) > 0 && exactObject[0]) {
			clauses = append(clauses, "d."+p.column+"_sha=UNHEX(SHA2(CAST(? AS BINARY),256)) AND CAST(d."+p.column+" AS BINARY)=CAST(? AS BINARY)")
			args = append(args, mysqlstore.Text(p.value), mysqlstore.Text(p.value))
		}
	}
	if f.Outcome != "" {
		clauses = append(clauses, "CAST(d.outcome AS BINARY)=CAST(? AS BINARY)")
		args = append(args, mysqlstore.Text(f.Outcome))
	}
	if f.ObjectIDPrefix != "" {
		clauses = append(clauses, "d.object_id LIKE ? COLLATE utf8mb4_0900_bin")
		args = append(args, mysqlstore.Text(f.ObjectIDPrefix+"%"))
	}
	for _, p := range []struct {
		op    string
		value *int
	}{{">=", f.MinScore}, {"<=", f.MaxScore}} {
		if p.value != nil {
			n, err := checkedInt(*p.value)
			if err != nil {
				return "", nil, err
			}
			clauses = append(clauses, "d.score"+p.op+"?")
			args = append(args, n)
		}
	}
	join := ""
	if f.Search != "" {
		// Approved MySQL Unicode case-insensitive/accent-sensitive search, not an assertion
		// of equivalence to every deployment-specific PostgreSQL ILIKE locale.
		join = " LEFT JOIN " + r.schema + ".decision_engine_scenarios s ON s.tenant_id=d.tenant_id AND s.id=d.scenario_id"
		search := []string{}
		for _, column := range []string{"BIN_TO_UUID(d.id)", "d.object_id", "d.object_type", "COALESCE(s.name,'')"} {
			search = append(search, column+" COLLATE utf8mb4_0900_as_ci LIKE ? COLLATE utf8mb4_0900_as_ci")
			args = append(args, mysqlstore.Text("%"+f.Search+"%"))
		}
		clauses = append(clauses, "("+strings.Join(search, " OR ")+")")
	}
	return join + " WHERE " + strings.Join(clauses, " AND "), args, nil
}
func (r DecisionRepository) ListFiltered(ctx context.Context, tenant string, f ports.DecisionListFilter) ([]decision.Decision, error) {
	return r.listFiltered(ctx, tenant, f, false)
}
func (r DecisionRepository) listFiltered(ctx context.Context, tenant string, f ports.DecisionListFilter, exactObject bool) ([]decision.Decision, error) {
	where, args, err := r.filter(tenant, f, exactObject)
	if err != nil {
		return nil, err
	}
	return platformList(ctx, r.platformRepository, r.selectSQL(false)+where+" ORDER BY d.created_at DESC,d.id DESC", args, r.scan(false))
}
func (r DecisionRepository) page(ctx context.Context, tenant string, f ports.DecisionListFilter, limit, offset int, cursor *ports.DecisionListCursor, exactObject ...bool) ([]decision.Decision, bool, error) {
	if limit < 1 || limit > r.maxRows || offset < 0 {
		return nil, false, ErrProjectionLimit
	}
	where, args, err := r.filter(tenant, f, exactObject...)
	if err != nil {
		return nil, false, err
	}
	if cursor != nil {
		ids, err := r.ids(tenant, cursor.ID)
		if err != nil {
			return nil, false, err
		}
		at := mysqlstore.DateTime{Time: cursor.CreatedAt}
		if _, err = at.Value(); err != nil {
			return nil, false, err
		}
		where += " AND (d.created_at<? OR (d.created_at=? AND d.id<?))"
		args = append(args, at, at, ids[1])
	}
	// One lookahead row is intentional for hasMore; both it and page rows are
	// covered by the byte budget. No full request JSON is fetched for lists.
	rows, err := r.tx.QueryContext(ctx, r.selectSQL(false)+where+" ORDER BY d.created_at DESC,d.id DESC LIMIT ? OFFSET ?", append(args, limit+1, offset)...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []decision.Decision{}
	var used int64
	for rows.Next() {
		v, size, err := r.scan(false)(rows)
		if err != nil {
			return nil, false, err
		}
		if size < 0 || size > int64(r.maxBytes)-used {
			return nil, false, ErrProjectionLimit
		}
		used += size
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(out) > limit {
		return out[:limit], true, nil
	}
	return out, false, nil
}
func (r DecisionRepository) ListFilteredPage(ctx context.Context, t string, f ports.DecisionListFilter, l, o int) ([]decision.Decision, bool, error) {
	return r.page(ctx, t, f, l, o, nil)
}
func (r DecisionRepository) ListFilteredCursor(ctx context.Context, t string, f ports.DecisionListFilter, l int, c *ports.DecisionListCursor) ([]decision.Decision, bool, error) {
	return r.page(ctx, t, f, l, 0, c)
}
func (r DecisionRepository) CountFiltered(ctx context.Context, t string, f ports.DecisionListFilter) (int, error) {
	return r.countFiltered(ctx, t, f, false)
}
func (r DecisionRepository) countFiltered(ctx context.Context, t string, f ports.DecisionListFilter, exactObject bool) (int, error) {
	where, args, err := r.filter(t, f, exactObject)
	if err != nil {
		return 0, err
	}
	var count int64
	if err := r.tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+r.schema+".decision_engine_decisions d"+where, args...).Scan(&count); err != nil {
		return 0, err
	}
	if count < 0 || int64(int(count)) != count {
		return 0, errors.New("decision count overflow")
	}
	return int(count), nil
}
func (r DecisionRepository) ListByTenant(c context.Context, t string) ([]decision.Decision, error) {
	return r.ListFiltered(c, t, ports.DecisionListFilter{})
}
func (r DecisionRepository) ListByTenantPage(c context.Context, t string, l, o int) ([]decision.Decision, bool, error) {
	return r.ListFilteredPage(c, t, ports.DecisionListFilter{}, l, o)
}
func (r DecisionRepository) CountByTenant(c context.Context, t string) (int, error) {
	return r.CountFiltered(c, t, ports.DecisionListFilter{})
}
func (r DecisionRepository) ListByScenario(c context.Context, t, s string) ([]decision.Decision, error) {
	if _, err := r.ids(t, s); err != nil {
		return nil, err
	}
	return r.ListFiltered(c, t, ports.DecisionListFilter{ScenarioID: s})
}
func (r DecisionRepository) ListByScenarioPage(c context.Context, t, s string, l, o int) ([]decision.Decision, bool, error) {
	if _, err := r.ids(t, s); err != nil {
		return nil, false, err
	}
	return r.ListFilteredPage(c, t, ports.DecisionListFilter{ScenarioID: s}, l, o)
}
func (r DecisionRepository) CountByScenario(c context.Context, t, s string) (int, error) {
	if _, err := r.ids(t, s); err != nil {
		return 0, err
	}
	return r.CountFiltered(c, t, ports.DecisionListFilter{ScenarioID: s})
}
func (r DecisionRepository) ListByObject(c context.Context, t, k, id string) ([]decision.Decision, error) {
	return r.listFiltered(c, t, ports.DecisionListFilter{ObjectType: k, ObjectID: id}, true)
}
func (r DecisionRepository) ListByObjectPage(c context.Context, t, k, id string, l, o int) ([]decision.Decision, bool, error) {
	return r.page(c, t, ports.DecisionListFilter{ObjectType: k, ObjectID: id}, l, o, nil, true)
}
func (r DecisionRepository) CountByObject(c context.Context, t, k, id string) (int, error) {
	return r.countFiltered(c, t, ports.DecisionListFilter{ObjectType: k, ObjectID: id}, true)
}
