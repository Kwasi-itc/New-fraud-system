package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ExplorationRepository struct{ db queryable }
type explorationStep struct {
	From, To                             uuid.UUID
	FromName, ToName, FromField, ToField string
}
type explorationEdge struct {
	casepkg.ExplorationConnection
	steps []explorationStep
}

// All identifiers come from tenant-validated owner metadata and are quoted.
// Object IDs and cursor values are always parameters, never SQL fragments.
func (r ExplorationRepository) Explore(ctx context.Context, tenant uuid.UUID, root casepkg.DecisionReference, in casepkg.ExplorationRequest) (casepkg.Exploration, error) {
	out := casepkg.Exploration{Connections: []casepkg.ExplorationConnection{}, Records: []casepkg.ExplorationRecord{}}
	var schema, name string
	var table uuid.UUID
	err := r.db.QueryRow(ctx, `SELECT t.schema_name,m.id,m.name FROM core.tenants t JOIN core.model_tables m ON m.tenant_id=t.id WHERE t.id=$1 AND t.status='active' AND m.name=$2 AND NOT m.archived`, tenant, root.ObjectType).Scan(&schema, &table, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, casepkg.ErrNotFound
	}
	if err != nil {
		return out, err
	}
	object := root.ObjectID
	for _, hop := range in.Path {
		edges, err := r.edges(ctx, tenant, table)
		if err != nil {
			return out, err
		}
		edge, ok := findExplorationEdge(edges, hop.Connection)
		if !ok {
			return out, casepkg.ErrForbidden
		}
		rows, err := r.neighbors(ctx, schema, edge, object, "", hop.ObjectID)
		if err != nil {
			return out, err
		}
		if len(rows) != 1 {
			return out, casepkg.ErrNotFound
		}
		end := edge.steps[len(edge.steps)-1]
		table, name, object = end.To, end.ToName, hop.ObjectID
	}
	out.Table = name
	out.Current, err = r.record(ctx, tenant, schema, table, name, object)
	if err != nil {
		return out, err
	}
	edges, err := r.edges(ctx, tenant, table)
	if err != nil {
		return out, err
	}
	for _, edge := range edges {
		out.Connections = append(out.Connections, edge.ExplorationConnection)
	}
	if in.Connection != "" {
		edge, ok := findExplorationEdge(edges, in.Connection)
		if !ok {
			return out, casepkg.ErrForbidden
		}
		out.Records, err = r.neighbors(ctx, schema, edge, object, in.After, "")
		if err != nil {
			return out, err
		}
		if len(out.Records) > 25 {
			out.Records = out.Records[:25]
			out.Next = out.Records[24].ObjectID
		}
	}
	return out, nil
}
func findExplorationEdge(edges []explorationEdge, id string) (explorationEdge, bool) {
	for _, e := range edges {
		if e.ID == id {
			return e, true
		}
	}
	return explorationEdge{}, false
}

func (r ExplorationRepository) record(ctx context.Context, tenant uuid.UUID, schema string, table uuid.UUID, name, object string) (casepkg.ExplorationRecord, error) {
	out := casepkg.ExplorationRecord{ObjectID: object}
	// Whitelist published, active fields. Internal/archived columns cannot leak
	// merely because they still exist in the physical tenant table.
	query := fmt.Sprintf(`SELECT CASE WHEN octet_length(data::text)<=131072 THEN data ELSE NULL END FROM %s rec
 CROSS JOIN LATERAL (SELECT coalesce(jsonb_object_agg(e.key,e.value),'{}'::jsonb) data FROM jsonb_each(to_jsonb(rec)) e
 WHERE e.key IN ('object_id','updated_at') OR EXISTS(SELECT 1 FROM core.model_fields f WHERE f.tenant_id=$1 AND f.table_id=$2 AND f.name=e.key AND NOT f.archived)) selected
 WHERE rec.object_id=$3 LIMIT 1`, pgx.Identifier{schema, name}.Sanitize())
	err := r.db.QueryRow(ctx, query, tenant, table, object).Scan(&out.Data)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, casepkg.ErrNotFound
	}
	if err == nil && out.Data == nil {
		return out, casepkg.Invalid("record exceeds investigation display limit")
	}
	return out, err
}

func (r ExplorationRepository) neighbors(ctx context.Context, schema string, edge explorationEdge, object, after, target string) ([]casepkg.ExplorationRecord, error) {
	from := pgx.Identifier{schema, edge.steps[0].FromName}.Sanitize() + " s0"
	for n, step := range edge.steps {
		from += fmt.Sprintf(" JOIN %s s%d ON s%d.%s=s%d.%s AND s%d.%s::text<>''", pgx.Identifier{schema, step.ToName}.Sanitize(), n+1, n, pgx.Identifier{step.FromField}.Sanitize(), n+1, pgx.Identifier{step.ToField}.Sanitize(), n, pgx.Identifier{step.FromField}.Sanitize())
	}
	last := fmt.Sprintf("s%d.object_id", len(edge.steps))
	query := fmt.Sprintf(`SELECT DISTINCT %s FROM %s WHERE s0.object_id=$1 AND %s>$2`, last, from, last)
	args := []any{object, after}
	if target != "" {
		query += " AND " + last + "=$3"
		args = append(args, target)
	}
	if strings.HasPrefix(edge.ID, "pivot:") && len(edge.steps) == 1 && edge.steps[0].From == edge.steps[0].To && edge.steps[0].FromField == edge.steps[0].ToField {
		query += " AND " + last + "<>$1"
	}
	query += " ORDER BY " + last + " LIMIT 26"
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []casepkg.ExplorationRecord{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, casepkg.ExplorationRecord{ObjectID: id, Data: json.RawMessage(`{}`)})
	}
	return out, rows.Err()
}

// Shared metadata projection validates BOTH endpoint tables and fields against
// the tenant, archive flags and matching data types before allowing traversal.
const explorationEndpoints = ` JOIN core.model_tables st ON st.id=e.source_table AND st.tenant_id=$1 AND NOT st.archived
 JOIN core.model_tables tt ON tt.id=e.target_table AND tt.tenant_id=$1 AND NOT tt.archived
 JOIN core.model_fields sf ON sf.id=e.source_field AND sf.table_id=st.id AND sf.tenant_id=$1 AND NOT sf.archived
 JOIN core.model_fields tf ON tf.id=e.target_field AND tf.table_id=tt.id AND tf.tenant_id=$1 AND NOT tf.archived AND tf.data_type=sf.data_type `
const explorationLinkProjection = `SELECT l.id,l.name,l.child_table_id source_table,l.parent_table_id target_table,l.child_field_id source_field,l.parent_field_id target_field FROM core.model_links l WHERE l.tenant_id=$1`

func (r ExplorationRepository) edges(ctx context.Context, tenant, table uuid.UUID) ([]explorationEdge, error) {
	rows, err := r.db.Query(ctx, `WITH e AS (
 SELECT 'link:'||id::text key,name label,child_table_id source_table,parent_table_id target_table,child_field_id source_field,parent_field_id target_field FROM core.model_links WHERE tenant_id=$1 AND child_table_id=$2
 UNION ALL SELECT 'reverse:'||id::text,name||' (reverse)',parent_table_id,child_table_id,parent_field_id,child_field_id FROM core.model_links WHERE tenant_id=$1 AND parent_table_id=$2
 UNION ALL SELECT 'navigation:'||id::text,'Navigation',source_table_id,target_table_id,source_field_id,filter_field_id FROM core.navigation_options WHERE tenant_id=$1 AND source_table_id=$2
 UNION ALL SELECT 'pivot:'||id::text,'Shared pivot',base_table_id,base_table_id,field_id,field_id FROM core.model_pivots WHERE tenant_id=$1 AND base_table_id=$2 AND field_id IS NOT NULL AND cardinality(path_link_ids)=0
 ) SELECT e.key,e.label,st.id,tt.id,st.name,tt.name,sf.name,tf.name FROM e `+explorationEndpoints+` ORDER BY e.key LIMIT 201`, tenant, table)
	if err != nil {
		return nil, err
	}
	out := []explorationEdge{}
	for rows.Next() {
		var e explorationEdge
		var step explorationStep
		if err = rows.Scan(&e.ID, &e.Label, &step.From, &step.To, &step.FromName, &step.ToName, &step.FromField, &step.ToField); err != nil {
			rows.Close()
			return nil, err
		}
		e.Target = step.ToName
		e.Reason = step.FromName + "." + step.FromField + " = " + step.ToName + "." + step.ToField
		e.steps = []explorationStep{step}
		out = append(out, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(out) > 200 {
		return nil, casepkg.Invalid("too many configured connections")
	}
	// Path pivots lead to their terminal parent record. From there, reverse links
	// expose other records sharing that pivot. Fetch all path definitions in one
	// batch; do not issue a query per displayed relationship.
	rows, err = r.db.Query(ctx, `SELECT id,path_link_ids FROM core.model_pivots WHERE tenant_id=$1 AND base_table_id=$2 AND cardinality(path_link_ids)>0 ORDER BY id LIMIT 201`, tenant, table)
	if err != nil {
		return nil, err
	}
	type pathPivot struct {
		id    uuid.UUID
		links []uuid.UUID
	}
	pivots := []pathPivot{}
	ids := []uuid.UUID{}
	for rows.Next() {
		var p pathPivot
		if err = rows.Scan(&p.id, &p.links); err != nil {
			rows.Close()
			return nil, err
		}
		if len(p.links) > 6 {
			rows.Close()
			return nil, casepkg.Invalid("pivot path exceeds six links")
		}
		pivots = append(pivots, p)
		ids = append(ids, p.links...)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(out)+len(pivots) > 200 {
		return nil, casepkg.Invalid("too many configured connections")
	}
	if len(pivots) == 0 {
		return out, nil
	}
	rows, err = r.db.Query(ctx, `WITH e AS (`+explorationLinkProjection+`) SELECT e.id,st.id,tt.id,st.name,tt.name,sf.name,tf.name FROM e `+explorationEndpoints+` WHERE e.id=ANY($2)`, tenant, ids)
	if err != nil {
		return nil, err
	}
	links := map[uuid.UUID]explorationStep{}
	for rows.Next() {
		var id uuid.UUID
		var s explorationStep
		if err = rows.Scan(&id, &s.From, &s.To, &s.FromName, &s.ToName, &s.FromField, &s.ToField); err != nil {
			rows.Close()
			return nil, err
		}
		links[id] = s
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, p := range pivots {
		steps := []explorationStep{}
		current := table
		reason := []string{}
		for _, id := range p.links {
			s, ok := links[id]
			if !ok || s.From != current {
				steps = nil
				break
			}
			steps = append(steps, s)
			current = s.To
			reason = append(reason, s.FromName+"."+s.FromField+" = "+s.ToName+"."+s.ToField)
		}
		if len(steps) == 0 {
			continue
		}
		last := steps[len(steps)-1]
		out = append(out, explorationEdge{casepkg.ExplorationConnection{ID: "pivot:" + p.id.String(), Label: "Follow pivot path", Target: last.ToName, Reason: strings.Join(reason, " → ")}, steps})
	}
	return out, nil
}
