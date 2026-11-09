package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestMySQLRealIdentityRepresentativeListPlans(t *testing.T) {
	f, admin, tenant := listAdminFixture(t)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.database)
	var hot uuid.UUID
	for tenantIndex := 0; tenantIndex < 5; tenantIndex++ {
		current := admin
		if tenantIndex > 0 {
			other := uuid.New()
			session := seedIdentity(t, f, other, "platform_builder")
			auth, _ := SessionAuthorizer(f.database, session)
			owner, err := NewCustomListStore(f.db, f.database, other, 500, 2<<20, auth)
			if err != nil {
				t.Fatal(err)
			}
			current, err = NewCustomListAdmin(owner, admin.ids, admin.clock)
			if err != nil {
				t.Fatal(err)
			}
		}
		for listIndex := 0; listIndex < 8; listIndex++ {
			list, err := current.Save(ctx, uuid.Nil, adminList(fmt.Sprintf("list-%d", listIndex)))
			if err != nil {
				t.Fatal(err)
			}
			id := uuid.MustParse(list.ID)
			if tenantIndex == 0 && listIndex == 0 {
				hot = id
			}
			values := make([]string, 256)
			for i := range values {
				values[i] = fmt.Sprintf("value-%04d", i)
			}
			if n, err := current.Import(ctx, id, values); err != nil || n != len(values) {
				t.Fatal(n, err)
			}
		}
	}
	for batch := 1; batch < 16; batch++ {
		values := make([]string, 256)
		for i := range values {
			values[i] = fmt.Sprintf("value-%04d", batch*256+i)
		}
		if _, err := admin.Import(ctx, hot, values); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.db.ExecContext(ctx, "ANALYZE TABLE "+schema+".decision_engine_custom_lists,"+schema+".decision_engine_custom_list_entries"); err != nil {
		t.Fatal(err)
	}
	t.Log("Representative bounded fixture: 5 tenants, 40 lists, 14080 linked entries; one hot list has 4096 unique values. No production latency/SLA claim.")
	if err := admin.owner.ReadSnapshot(ctx, func(r CustomListRepository) error {
		plans := []struct {
			name, q string
			args    []any
			entry   bool
		}{
			{"hot_name_membership", "SELECT EXISTS(SELECT 1 FROM " + schema + ".decision_engine_custom_lists l JOIN " + schema + ".decision_engine_custom_list_entries e ON e.tenant_id=l.tenant_id AND e.list_id=l.id WHERE l.tenant_id=? AND " + exactListMatch + " AND e.value_sha=UNHEX(SHA2(CAST(? AS BINARY),256)) AND CAST(e.value AS BINARY)=CAST(? AS BINARY))", []any{mysqlstore.UUID(tenant), "list-0", "list-0", "value-4095", "value-4095"}, true},
			{"id_lookup", r.listSQL() + " WHERE tenant_id=? AND id=?", []any{r.maxBytes, mysqlstore.UUID(tenant), mysqlstore.UUID(hot)}, false},
			{"entry_page", r.entrySQL() + " WHERE e.tenant_id=? AND e.list_id=? ORDER BY e.created_at DESC,e.id DESC LIMIT 51", []any{mysqlstore.UUID(tenant), mysqlstore.UUID(hot)}, true},
		}
		// Projection helpers inline the configured bound; no extra bind argument.
		plans[1].args = []any{mysqlstore.UUID(tenant), mysqlstore.UUID(hot)}
		for _, p := range plans {
			var raw string
			if err := r.tx.QueryRowContext(ctx, "EXPLAIN FORMAT=JSON "+p.q, p.args...).Scan(&raw); err != nil {
				return err
			}
			t.Logf("%s unhinted plan: %s", p.name, raw)
			var plan any
			if err := json.Unmarshal([]byte(raw), &plan); err != nil {
				return err
			}
			found := false
			var visit func(any)
			visit = func(v any) {
				switch x := v.(type) {
				case map[string]any:
					if x["table_name"] == "e" {
						found = true
						if x["access_type"] == "ALL" || x["key"] == nil {
							t.Errorf("%s scans entry table: %v", p.name, x)
						}
					}
					for _, child := range x {
						visit(child)
					}
				case []any:
					for _, child := range x {
						visit(child)
					}
				}
			}
			visit(plan)
			if p.entry && !found {
				t.Errorf("missing entry plan for %s", p.name)
			}
		}
		found, err := r.Contains(ctx, tenant.String(), "list-0", "value-4095")
		if err != nil {
			return err
		}
		if !found {
			t.Fatal("hot name membership missing")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Entries(ctx, hot); !errors.Is(err, ErrProjectionLimit) {
		t.Fatal("hot list row overflow must be explicit", err)
	}
	tiny, err := NewCustomListStore(f.db, f.database, tenant, 500, 128, admin.owner.configuration.authorize)
	if err != nil {
		t.Fatal(err)
	}
	if err := tiny.ReadSnapshot(ctx, func(r CustomListRepository) error {
		_, err := r.Contains(ctx, tenant.String(), "list-0", strings.Repeat("x", 129))
		return err
	}); !errors.Is(err, ErrProjectionLimit) {
		t.Fatal("lookup byte overflow", err)
	}
}
