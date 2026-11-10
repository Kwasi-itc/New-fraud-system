package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestMySQLCustomListSummariesCountsScopeAndAuthority(t *testing.T) {
	f, s, tenant := listAdminFixture(t)
	ctx := context.Background()
	list, err := s.Save(ctx, uuid.Nil, adminList("Risk"))
	if err != nil {
		t.Fatal(err)
	}
	empty, err := s.Save(ctx, uuid.Nil, adminList("Empty"))
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.MustParse(list.ID)
	first, err := s.SaveEntry(ctx, id, uuid.Nil, "first")
	if err != nil {
		t.Fatal(err)
	}
	for batch := 0; batch < 2; batch++ {
		values := make([]string, 300)
		for i := range values {
			values[i] = fmt.Sprintf("batch-%d-value-%d", batch, i)
		}
		if n, err := s.Import(ctx, id, values); err != nil || n != 300 {
			t.Fatal(n, err)
		}
	}
	// Counting remains available even when downloading all values exceeds the
	// entry projection budget. The overview must not depend on entry contents.
	if _, err := s.Entries(ctx, id); !errors.Is(err, ErrProjectionLimit) {
		t.Fatal(err)
	}
	foreignOwner, err := NewCustomListStore(f.db, f.database, uuid.New(), 500, 2<<20, allowAuthoringFixture)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := NewCustomListAdmin(foreignOwner, serviceIDsFixture{}, s.clock)
	if err != nil {
		t.Fatal(err)
	}
	foreignList, err := foreign.Save(ctx, uuid.Nil, adminList("Risk"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := foreign.Import(ctx, uuid.MustParse(foreignList.ID), []string{"foreign"}); err != nil {
		t.Fatal(err)
	}
	check := func(want int64) {
		t.Helper()
		rows, err := s.Summaries(ctx)
		if err != nil || len(rows) != 2 {
			t.Fatal(rows, err)
		}
		counts := map[string]int64{}
		for _, row := range rows {
			if row.TenantID != tenant.String() {
				t.Fatal("foreign tenant", row)
			}
			counts[row.ID] = row.EntryCount
		}
		if counts[list.ID] != want || counts[empty.ID] != 0 {
			t.Fatal(counts)
		}
	}
	check(601)
	var planJSON string
	if err := s.owner.ReadSnapshot(ctx, func(r CustomListRepository) error {
		return r.tx.QueryRowContext(ctx, "EXPLAIN FORMAT=JSON "+r.listSummarySQL()+" LIMIT ?", mysqlstore.UUID(tenant), 501).Scan(&planJSON)
	}); err != nil {
		t.Fatal(err)
	}
	var plan any
	if err := json.Unmarshal([]byte(planJSON), &plan); err != nil {
		t.Fatal(err)
	}
	indexedCount := false
	var inspect func(any)
	inspect = func(node any) {
		switch value := node.(type) {
		case map[string]any:
			if value["table_name"] == "e" {
				parts, _ := value["used_key_parts"].([]any)
				indexedCount = value["access_type"] == "ref" && len(parts) >= 2 && parts[0] == "tenant_id" && parts[1] == "list_id" && value["using_index"] == true
				t.Logf("count lookup: access=%v key=%v parts=%v covering=%v", value["access_type"], value["key"], parts, value["using_index"])
			}
			for _, child := range value {
				inspect(child)
			}
		case []any:
			for _, child := range value {
				inspect(child)
			}
		}
	}
	inspect(plan)
	if !indexedCount {
		t.Fatal("count query lost its covering tenant/list lookup", planJSON)
	}
	if _, err := s.Save(ctx, id, adminList("Renamed")); err != nil {
		t.Fatal(err)
	}
	check(601)
	if err := s.DeleteEntry(ctx, id, uuid.MustParse(first.ID)); err != nil {
		t.Fatal(err)
	}
	check(600)
	schema, err := mysqlstore.Identifier(f.database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("UPDATE "+schema+".identity_memberships SET active=FALSE,version=version+1 WHERE tenant_id=?", mysqlstore.UUID(tenant)); err != nil {
		t.Fatal(err)
	}
	if rows, err := s.Summaries(ctx); !errors.Is(err, ErrDecisionAuthorizationDenied) || rows != nil {
		t.Fatal("revoked membership returned counts", rows, err)
	}
}
