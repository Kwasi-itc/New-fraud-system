package integrationtest

import (
	"fmt"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// A private consumer fixture uses the real owner metadata, registry constraints,
// exact-key resolver and record lookup. It does not simulate owner authoring or
// claim coverage of the data-model publication worker.
func seedExplorerModel(t *testing.T, f *caseFixture) uuid.UUID {
	t.Helper()
	if !strings.HasPrefix(f.database, "case_fixture_") {
		t.Fatal("private case fixture required")
	}
	schema := "`" + f.database + "`"
	now := time.Now().UTC()
	_, e := f.db.ExecContext(f.ctx, "INSERT INTO "+schema+".data_model_tenants(id,name,schema_name,status,created_at,updated_at) VALUES(?,'Private investigation records',?,'active',?,?)", id(f.tenant), "case_"+strings.ReplaceAll(f.tenant.String(), "-", ""), now, now)
	must(t, e)
	type table struct {
		id             uuid.UUID
		name, physical string
		fields         map[string]uuid.UUID
		columns        map[string]string
	}
	tables := map[string]table{}
	for _, name := range []string{"transactions", "customers"} {
		v := table{id: uuid.New(), name: name, fields: map[string]uuid.UUID{}, columns: map[string]string{}}
		_, e = f.db.ExecContext(f.ctx, "INSERT INTO "+schema+".data_model_tables(id,tenant_id,name,created_at,updated_at) VALUES(?,?,?,?,?)", id(v.id), id(f.tenant), name, now, now)
		must(t, e)
		_, e = f.db.ExecContext(f.ctx, "INSERT INTO "+schema+".data_model_physical_tables(tenant_id,table_id,tenant_label,table_label,published_version,epoch,write_state) VALUES(?,?,'cmtest',?, ?,1,'open')", id(f.tenant), id(v.id), name, id(uuid.New()))
		must(t, e)
		must(t, f.db.QueryRowContext(f.ctx, "SELECT physical_name FROM "+schema+".data_model_physical_tables WHERE tenant_id=? AND table_id=?", id(f.tenant), id(v.id)).Scan(&v.physical))
		fields := []string{"object_id", "updated_at", "name", "source_ip"}
		if name == "transactions" {
			fields = []string{"object_id", "updated_at", "amount", "customer_id", "private_note", "source_ip", "nullable_flag"}
		}
		definition := []string{"id BINARY(16) PRIMARY KEY", "record_object_key_id BINARY(16) NOT NULL UNIQUE", "internal_private TEXT", "CONSTRAINT cm_fixture_object_" + strings.ReplaceAll(v.id.String(), "-", "") + " FOREIGN KEY(record_object_key_id) REFERENCES " + schema + ".data_model_exact_keys(id)"}
		for n, field := range fields {
			fieldID := uuid.New()
			kind, sqlType := "string", "VARCHAR(200)"
			if field == "updated_at" {
				kind, sqlType = "timestamp", "DATETIME(6)"
			}
			if field == "amount" {
				kind, sqlType = "int", "BIGINT"
			}
			if field == "source_ip" {
				kind = "ip_address"
			}
			if field == "nullable_flag" {
				kind, sqlType = "bool", "TINYINT"
			}
			archived := field == "private_note"
			_, e = f.db.ExecContext(f.ctx, "INSERT INTO "+schema+".data_model_fields(id,tenant_id,table_id,name,data_type,nullable,is_unique,archived,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)", id(fieldID), id(f.tenant), id(v.id), field, kind, field == "nullable_flag", field == "object_id" || (name == "customers" && field == "source_ip"), archived, now, now)
			must(t, e)
			v.fields[field] = fieldID
			components := []struct{ name, sql string }{{"value", sqlType}}
			if kind == "ip_address" {
				components = []struct{ name, sql string }{{"family", "TINYINT UNSIGNED"}, {"address", "VARBINARY(16)"}, {"prefix", "TINYINT UNSIGNED"}}
			}
			for _, component := range components {
				_, e = f.db.ExecContext(f.ctx, "INSERT INTO "+schema+".data_model_physical_fields(tenant_id,table_id,field_id,component,field_label) VALUES(?,?,?,?,?)", id(f.tenant), id(v.id), id(fieldID), component.name, fmt.Sprintf("cm%d", n))
				must(t, e)
				var column string
				must(t, f.db.QueryRowContext(f.ctx, "SELECT physical_name FROM "+schema+".data_model_physical_fields WHERE tenant_id=? AND table_id=? AND field_id=? AND component=?", id(f.tenant), id(v.id), id(fieldID), component.name).Scan(&column))
				quoted, e := mysqlstore.Identifier(column)
				must(t, e)
				key := field
				if component.name != "value" {
					key += "." + component.name
				}
				v.columns[key] = quoted
				definition = append(definition, quoted+" "+component.sql)
			}
		}
		if name == "customers" {
			definition = append(definition, "UNIQUE KEY cm_fixture_ip("+v.columns["source_ip.family"]+","+v.columns["source_ip.address"]+","+v.columns["source_ip.prefix"]+")")
		}
		_, e = f.db.ExecContext(f.ctx, "CREATE TABLE "+schema+".`"+v.physical+"`("+strings.Join(definition, ",")+") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin")
		must(t, e)
		tables[name] = v
	}
	parent, child := tables["customers"], tables["transactions"]
	link := uuid.New()
	_, e = f.db.ExecContext(f.ctx, "INSERT INTO "+schema+".data_model_links(id,tenant_id,name,parent_table_id,parent_field_id,child_table_id,child_field_id,created_at) VALUES(?,?,'Customer account',?,?,?,?,?)", id(link), id(f.tenant), id(parent.id), id(parent.fields["object_id"]), id(child.id), id(child.fields["customer_id"]), now)
	must(t, e)
	_, e = f.db.ExecContext(f.ctx, "INSERT INTO "+schema+".data_model_links(id,tenant_id,name,parent_table_id,parent_field_id,child_table_id,child_field_id,created_at) VALUES(?,?,'Source address',?,?,?,?,?)", id(uuid.New()), id(f.tenant), id(parent.id), id(parent.fields["source_ip"]), id(child.id), id(child.fields["source_ip"]), now)
	must(t, e)
	for _, v := range []table{parent, child} {
		object := "customer-001"
		if v.name == "transactions" {
			object = "synthetic-transfer-001"
		}
		conn, e := f.db.Conn(f.ctx)
		must(t, e)
		_, e = conn.ExecContext(f.ctx, "CALL "+schema+".data_model_resolve_exact_key(_binary'record_object_id',CONCAT(?,?),?,@cm_fixture_key)", id(f.tenant), id(v.id), []byte(object))
		must(t, e)
		var key mysqlstore.UUID
		must(t, conn.QueryRowContext(f.ctx, "SELECT @cm_fixture_key").Scan(&key))
		conn.Close()
		columns := []string{"id", "record_object_key_id", "internal_private", v.columns["object_id"], v.columns["updated_at"]}
		a := []any{id(uuid.New()), key, "must never be exposed", object, now}
		if v.name == "transactions" {
			columns = append(columns, v.columns["amount"], v.columns["customer_id"], v.columns["private_note"])
			a = append(a, int64(75000), "customer-001", "archived secret")
			columns = append(columns, v.columns["nullable_flag"])
			a = append(a, nil)
		} else {
			columns = append(columns, v.columns["name"])
			a = append(a, "Synthetic customer")
		}
		ip, e := mysqlstore.EncodeInet(netip.MustParsePrefix("192.0.2.17/24"))
		must(t, e)
		columns = append(columns, v.columns["source_ip.family"], v.columns["source_ip.address"], v.columns["source_ip.prefix"])
		a = append(a, ip.Family, ip.Address, ip.PrefixBits)
		_, e = f.db.ExecContext(f.ctx, "INSERT INTO "+schema+".`"+v.physical+"`("+strings.Join(columns, ",")+") VALUES("+strings.TrimSuffix(strings.Repeat("?,", len(a)), ",")+")", a...)
		must(t, e)
	}
	return link
}
