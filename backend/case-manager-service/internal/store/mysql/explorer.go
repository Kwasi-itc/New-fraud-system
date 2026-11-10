package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	c "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type explorerRepo struct{ scope }
type exploreField struct {
	id         uuid.UUID
	name, kind string
	columns    map[string]string
}
type exploreTable struct {
	id, version    uuid.UUID
	name, physical string
	fields         map[uuid.UUID]exploreField
	object         exploreField
}
type exploreStep struct {
	from, to    uuid.UUID
	left, right uuid.UUID
}
type exploreEdge struct {
	c.ExplorationConnection
	steps []exploreStep
}

func (r explorerRepo) tables(ctx context.Context, t uuid.UUID) (map[uuid.UUID]exploreTable, error) {
	rows, e := r.db.QueryContext(ctx, "SELECT m.id,m.name,p.physical_name,p.published_version,f.id,f.name,f.data_type,pf.component,pf.physical_name FROM "+r.schema+".data_model_tables m JOIN "+r.schema+".data_model_tenants tenant ON tenant.id=m.tenant_id AND tenant.status='active' JOIN "+r.schema+".data_model_physical_tables p ON p.tenant_id=m.tenant_id AND p.table_id=m.id AND p.write_state='open' AND p.published_version IS NOT NULL JOIN "+r.schema+".data_model_fields f ON f.tenant_id=m.tenant_id AND f.table_id=m.id AND NOT f.archived JOIN "+r.schema+".data_model_physical_fields pf ON pf.tenant_id=f.tenant_id AND pf.table_id=f.table_id AND pf.field_id=f.id WHERE m.tenant_id=? AND NOT m.archived ORDER BY m.id,f.id,pf.component LIMIT 4001", id(t))
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[uuid.UUID]exploreTable{}
	count := 0
	for rows.Next() {
		count++
		if count > 4000 {
			return nil, c.Invalid("model exceeds investigation metadata limit")
		}
		var table, field, version uuid.UUID
		var name, physical, fieldName, kind, component, column string
		if e = rows.Scan(uid(&table), &name, &physical, uid(&version), uid(&field), &fieldName, &kind, &component, &column); e != nil {
			return nil, e
		}
		if _, e = mysqlstore.Identifier(physical); e != nil {
			return nil, e
		}
		quoted, e := mysqlstore.Identifier(column)
		if e != nil {
			return nil, e
		}
		tab, ok := out[table]
		if !ok {
			tab = exploreTable{id: table, version: version, name: name, physical: r.schema + ".`" + physical + "`", fields: map[uuid.UUID]exploreField{}}
		}
		f, ok := tab.fields[field]
		if !ok {
			f = exploreField{id: field, name: fieldName, kind: kind, columns: map[string]string{}}
		}
		f.columns[component] = quoted
		tab.fields[field] = f
		out[table] = tab
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	for key, tab := range out {
		for _, f := range tab.fields {
			if f.name == "object_id" {
				tab.object = f
			}
		}
		if tab.object.columns["value"] == "" {
			return nil, c.ErrUnavailable
		}
		out[key] = tab
	}
	return out, nil
}
func (r explorerRepo) edges(ctx context.Context, t uuid.UUID, tables map[uuid.UUID]exploreTable) (map[uuid.UUID][]exploreEdge, error) {
	out := map[uuid.UUID][]exploreEdge{}
	links := map[uuid.UUID]exploreStep{}
	rows, e := r.db.QueryContext(ctx, "SELECT id,name,child_table_id,parent_table_id,child_field_id,parent_field_id FROM "+r.schema+".data_model_links WHERE tenant_id=? ORDER BY id LIMIT 201", id(t))
	if e != nil {
		return nil, e
	}
	valid := func(s exploreStep) bool {
		from, fok := tables[s.from]
		to, tok := tables[s.to]
		a, aok := from.fields[s.left]
		b, bok := to.fields[s.right]
		if !fok || !tok || !aok || !bok || a.kind != b.kind {
			return false
		}
		components := []string{"value"}
		if a.kind == "ip_address" {
			components = []string{"family", "address", "prefix"}
		}
		for _, component := range components {
			if a.columns[component] == "" || b.columns[component] == "" {
				return false
			}
		}
		return true
	}
	add := func(key, label string, s exploreStep) {
		if !valid(s) {
			return
		}
		a, b := tables[s.from], tables[s.to]
		out[s.from] = append(out[s.from], exploreEdge{c.ExplorationConnection{ID: key, Label: label, Target: b.name, Reason: a.name + "." + a.fields[s.left].name + " = " + b.name + "." + b.fields[s.right].name}, []exploreStep{s}})
	}
	count := 0
	for rows.Next() {
		count++
		if count > 200 {
			rows.Close()
			return nil, c.Invalid("too many configured connections")
		}
		var key uuid.UUID
		var label string
		var s exploreStep
		if e = rows.Scan(uid(&key), &label, uid(&s.from), uid(&s.to), uid(&s.left), uid(&s.right)); e != nil {
			rows.Close()
			return nil, e
		}
		links[key] = s
		add("link:"+key.String(), label, s)
		add("reverse:"+key.String(), label+" (reverse)", exploreStep{s.to, s.from, s.right, s.left})
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	rows, e = r.db.QueryContext(ctx, "SELECT id,source_table_id,target_table_id,source_field_id,filter_field_id FROM "+r.schema+".data_model_navigation_options WHERE tenant_id=? ORDER BY id LIMIT 201", id(t))
	if e != nil {
		return nil, e
	}
	count = 0
	for rows.Next() {
		count++
		if count > 200 {
			rows.Close()
			return nil, c.Invalid("too many navigation connections")
		}
		var key uuid.UUID
		var s exploreStep
		if e = rows.Scan(uid(&key), uid(&s.from), uid(&s.to), uid(&s.left), uid(&s.right)); e != nil {
			rows.Close()
			return nil, e
		}
		add("navigation:"+key.String(), "Navigation", s)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	rows, e = r.db.QueryContext(ctx, "SELECT p.id,p.base_table_id,p.field_id,l.ordinal,l.link_id FROM "+r.schema+".data_model_pivots p LEFT JOIN "+r.schema+".data_model_pivot_path_links l ON l.tenant_id=p.tenant_id AND l.pivot_id=p.id WHERE p.tenant_id=? ORDER BY p.id,l.ordinal LIMIT 1401", id(t))
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	type pivot struct {
		key, base uuid.UUID
		field     *uuid.UUID
		steps     []exploreStep
		ok        bool
	}
	var current *pivot
	flush := func() error {
		if current == nil {
			return nil
		}
		if len(current.steps) == 0 && current.field != nil {
			add("pivot:"+current.key.String(), "Shared pivot", exploreStep{current.base, current.base, *current.field, *current.field})
			return nil
		}
		if !current.ok || len(current.steps) == 0 {
			return nil
		}
		if len(current.steps) > 6 {
			return c.Invalid("pivot path exceeds six links")
		}
		last := current.steps[len(current.steps)-1]
		out[current.base] = append(out[current.base], exploreEdge{c.ExplorationConnection{ID: "pivot:" + current.key.String(), Label: "Follow pivot path", Target: tables[last.to].name, Reason: "Configured pivot path"}, current.steps})
		return nil
	}
	count = 0
	for rows.Next() {
		count++
		if count > 1400 {
			return nil, c.Invalid("too many pivot paths")
		}
		var key, base uuid.UUID
		var field, link *uuid.UUID
		var ordinal *int
		if e = rows.Scan(uid(&key), uid(&base), nullableID{&field}, &ordinal, nullableID{&link}); e != nil {
			return nil, e
		}
		if current == nil || current.key != key {
			if e = flush(); e != nil {
				return nil, e
			}
			current = &pivot{key: key, base: base, field: field, ok: true}
		}
		if link != nil {
			s, ok := links[*link]
			expected := current.base
			if len(current.steps) > 0 {
				expected = current.steps[len(current.steps)-1].to
			}
			if !ok || !valid(s) || s.from != expected || ordinal == nil || *ordinal != len(current.steps) {
				current.ok = false
			}
			current.steps = append(current.steps, s)
		}
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	if e = flush(); e != nil {
		return nil, e
	}
	for _, v := range out {
		if len(v) > 200 {
			return nil, c.Invalid("too many configured connections")
		}
	}
	return out, nil
}
func (r explorerRepo) record(ctx context.Context, t uuid.UUID, tab exploreTable, object string) (c.ExplorationRecord, error) {
	out := c.ExplorationRecord{ObjectID: object}
	parts := []string{}
	a := []any{}
	for _, f := range tab.fields {
		expr := f.columns["value"]
		if f.kind == "ip_address" {
			for _, component := range []string{"family", "address", "prefix"} {
				if f.columns[component] == "" {
					return out, c.ErrUnavailable
				}
			}
			expr = "CONCAT(INET6_NTOA(" + f.columns["address"] + "),CASE WHEN(" + f.columns["family"] + "=4 AND " + f.columns["prefix"] + "=32) OR(" + f.columns["family"] + "=6 AND " + f.columns["prefix"] + "=128) THEN '' ELSE CONCAT('/'," + f.columns["prefix"] + ") END)"
		}
		if expr == "" {
			return out, c.ErrUnavailable
		}
		if f.kind == "bool" {
			expr = "IF(" + expr + " IS NULL,NULL,JSON_EXTRACT(IF(" + expr + ",'true','false'),'$'))"
		}
		if f.kind == "timestamp" {
			expr = "DATE_FORMAT(" + expr + ",'%Y-%m-%dT%H:%i:%s.%fZ')"
		}
		parts = append(parts, "?,"+expr)
		a = append(a, f.name)
	}
	a = append(a, id(t), id(tab.id), []byte(object), object)
	q := "SELECT IF(OCTET_LENGTH(data)<=131072,data,NULL) FROM(SELECT JSON_OBJECT(" + strings.Join(parts, ",") + ") data FROM " + tab.physical + " WHERE record_object_key_id=" + r.schema + ".data_model_find_record_object_key_v1(?,?,?) AND CAST(" + tab.object.columns["value"] + " AS BINARY)=CAST(? AS BINARY) LIMIT 1) record_projection"
	e := r.db.QueryRowContext(ctx, q, a...).Scan(&out.Data)
	if e == nil && (out.Data == nil || len(out.Data) > 131072) {
		return out, c.Invalid("record exceeds investigation display limit")
	}
	return out, normalized(e)
}
func (r explorerRepo) neighbors(ctx context.Context, t uuid.UUID, tables map[uuid.UUID]exploreTable, edge exploreEdge, object, after, target string) ([]c.ExplorationRecord, error) {
	first := tables[edge.steps[0].from]
	from := first.physical + " s0"
	for n, s := range edge.steps {
		a, b := tables[s.from], tables[s.to]
		leftField, rightField := a.fields[s.left], b.fields[s.right]
		alias := func(n int) string { return "s" + string(rune('0'+n)) }
		components := []string{"value"}
		if leftField.kind == "ip_address" {
			components = []string{"family", "address", "prefix"}
		}
		conditions := []string{}
		for _, component := range components {
			conditions = append(conditions, alias(n)+"."+leftField.columns[component]+"="+alias(n+1)+"."+rightField.columns[component])
		}
		if leftField.kind != "ip_address" {
			conditions = append(conditions, "CAST("+alias(n)+"."+leftField.columns["value"]+" AS CHAR)<>''")
		}
		from += " JOIN " + b.physical + " " + alias(n+1) + " ON " + strings.Join(conditions, " AND ")
	}
	last := tables[edge.steps[len(edge.steps)-1].to]
	col := "s" + string(rune('0'+len(edge.steps))) + "." + last.object.columns["value"]
	q := "SELECT DISTINCT " + col + " FROM " + from + " WHERE s0.record_object_key_id=" + r.schema + ".data_model_find_record_object_key_v1(?,?,?) AND CAST(s0." + first.object.columns["value"] + " AS BINARY)=CAST(? AS BINARY) AND " + col + ">?"
	a := []any{id(t), id(first.id), []byte(object), object, after}
	if target != "" {
		q += " AND " + col + "=?"
		a = append(a, target)
	}
	if strings.HasPrefix(edge.ID, "pivot:") && len(edge.steps) == 1 && edge.steps[0].from == edge.steps[0].to && edge.steps[0].left == edge.steps[0].right {
		q += " AND " + col + "<>?"
		a = append(a, object)
	}
	rows, e := r.db.QueryContext(ctx, q+" ORDER BY "+col+" LIMIT 26", a...)
	return collect(rows, e, func(r row) (c.ExplorationRecord, error) {
		var v c.ExplorationRecord
		e := r.Scan(&v.ObjectID)
		v.Data = json.RawMessage(`{}`)
		return v, e
	})
}
func (r explorerRepo) Explore(ctx context.Context, t uuid.UUID, root c.DecisionReference, in c.ExplorationRequest) (c.Exploration, error) {
	out := c.Exploration{Connections: []c.ExplorationConnection{}, Records: []c.ExplorationRecord{}}
	if root.ObjectID == "" {
		return out, c.ErrNotFound
	}
	tables, e := r.tables(ctx, t)
	if e != nil {
		return out, e
	}
	var tab exploreTable
	for _, v := range tables {
		if v.name == root.ObjectType {
			tab = v
			break
		}
	}
	if tab.id == uuid.Nil {
		return out, c.ErrNotFound
	}
	edges, e := r.edges(ctx, t, tables)
	if e != nil {
		return out, e
	}
	object := root.ObjectID
	find := func(table uuid.UUID, key string) (exploreEdge, bool) {
		for _, v := range edges[table] {
			if v.ID == key {
				return v, true
			}
		}
		return exploreEdge{}, false
	}
	for _, hop := range in.Path {
		edge, ok := find(tab.id, hop.Connection)
		if !ok {
			return out, c.ErrForbidden
		}
		records, e := r.neighbors(ctx, t, tables, edge, object, "", hop.ObjectID)
		if e != nil {
			return out, e
		}
		if len(records) != 1 {
			return out, c.ErrNotFound
		}
		tab = tables[edge.steps[len(edge.steps)-1].to]
		object = hop.ObjectID
	}
	out.Table = tab.name
	out.Current, e = r.record(ctx, t, tab, object)
	if e != nil {
		return out, e
	}
	for _, v := range edges[tab.id] {
		out.Connections = append(out.Connections, v.ExplorationConnection)
	}
	if in.Connection != "" {
		edge, ok := find(tab.id, in.Connection)
		if !ok {
			return out, c.ErrForbidden
		}
		out.Records, e = r.neighbors(ctx, t, tables, edge, object, in.After, "")
		if e != nil {
			return out, e
		}
		if len(out.Records) > 25 {
			out.Records = out.Records[:25]
			out.Next = out.Records[24].ObjectID
		}
	}
	return out, nil
}
func (r explorerRepo) Decisions(ctx context.Context, t uuid.UUID, table, object, user string, admin bool, before *c.Cursor) ([]c.ExplorationDecision, error) {
	u, e := uuid.Parse(user)
	if e != nil {
		return nil, c.ErrForbidden
	}
	q := "SELECT d.id,d.scenario_id,s.name,d.outcome,d.score,d.triggered,d.created_at,c.id,c.name,c.status FROM " + r.schema + ".decision_engine_decisions d JOIN " + r.schema + ".decision_engine_scenarios s ON s.tenant_id=d.tenant_id AND s.id=d.scenario_id LEFT JOIN " + r.table("case_decisions") + " link ON link.tenant_id=d.tenant_id AND link.decision_id=d.id LEFT JOIN " + r.table("cases") + " c ON c.tenant_id=link.tenant_id AND c.id=link.case_id WHERE d.tenant_id=? AND d.object_type_sha=UNHEX(SHA2(CAST(? AS BINARY),256)) AND d.object_id_sha=UNHEX(SHA2(CAST(? AS BINARY),256)) AND d.object_type=? AND d.object_id=? AND(link.id IS NULL OR(c.id IS NOT NULL AND(? OR EXISTS(SELECT 1 FROM " + r.table("inbox_users") + " m WHERE m.tenant_id=c.tenant_id AND m.inbox_id=c.inbox_id AND m.user_id=?))))"
	a := []any{id(t), table, object, table, object, admin, id(u)}
	if before != nil {
		q += " AND(d.created_at<? OR(d.created_at=? AND d.id<?))"
		a = append(a, before.At, before.At, id(before.ID))
	}
	rows, e := r.db.QueryContext(ctx, q+" ORDER BY d.created_at DESC,d.id DESC LIMIT 26", a...)
	return collect(rows, e, func(r row) (c.ExplorationDecision, error) {
		var v c.ExplorationDecision
		var key *uuid.UUID
		var name, status *string
		e := r.Scan(uid(&v.ID), uid(&v.ScenarioID), &v.ScenarioName, &v.Outcome, &v.Score, &v.Triggered, &v.CreatedAt, nullableID{&key}, &name, &status)
		if e == nil && key != nil {
			if name == nil || status == nil {
				return v, errors.New("case summary is incomplete")
			}
			v.Case = &c.ExplorationCase{ID: *key, Name: *name, Status: *status}
		}
		return v, e
	})
}
