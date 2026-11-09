package localtenants

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/testsupport"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestLocalTenantRegistryPaginationActivationAndRevocation(t *testing.T) {
	db, name := testsupport.DecisionMySQLDatabase(t)
	schema, _ := mysqlstore.Identifier(name)
	if _, err := db.Exec("CREATE TABLE " + schema + ".data_model_tenants(id BINARY(16) PRIMARY KEY,status VARCHAR(16) NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	active := map[uuid.UUID]bool{}
	for i := range 123 {
		id := uuid.New()
		status := "paused"
		if i%3 == 0 {
			status = "active"
			active[id] = true
		}
		if _, err := db.Exec("INSERT INTO "+schema+".data_model_tenants VALUES(?,?)", mysqlstore.UUID(id), status); err != nil {
			t.Fatal(err)
		}
	}
	registry, err := New(db, name)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var after uuid.UUID
	seen := map[uuid.UUID]bool{}
	pages := 0
	for {
		page, err := registry.Scan(ctx, after, 20)
		if err != nil || len(page) > 20 {
			t.Fatal(page, err)
		}
		pages++
		for _, entry := range page {
			if seen[entry.ID] {
				t.Fatal("cursor repeated", entry.ID)
			}
			seen[entry.ID] = true
			if entry.Active != active[entry.ID] {
				t.Fatal("wrong activation", entry)
			}
			after = entry.ID
		}
		if len(page) < 20 {
			break
		}
	}
	if len(seen) != 123 || pages != 7 {
		t.Fatal("unbounded or incomplete traversal", len(seen), pages)
	}
	var tenant uuid.UUID
	for id := range active {
		tenant = id
		break
	}
	authorize := func(id uuid.UUID) error {
		return mysqlstore.Snapshot(ctx, db, func(tx *sql.Tx) error { return registry.Authorize(ctx, tx, id) })
	}
	if err := authorize(tenant); err != nil {
		t.Fatal(err)
	}
	if err := registry.CheckActive(ctx, tenant); err != nil {
		t.Fatal("active admission", err)
	}
	for _, id := range []uuid.UUID{uuid.Nil, uuid.New()} {
		if err := registry.CheckActive(ctx, id); err != ErrDenied {
			t.Fatal("unknown admission", id, err)
		}
	}
	if err := authorize(uuid.New()); err != ErrDenied {
		t.Fatal("unknown tenant admitted", err)
	}
	if _, err := db.Exec("UPDATE "+schema+".data_model_tenants SET status='paused' WHERE id=?", mysqlstore.UUID(tenant)); err != nil {
		t.Fatal(err)
	}
	if err := authorize(tenant); err != ErrDenied {
		t.Fatal("revocation cached", err)
	}
	if err := registry.CheckActive(ctx, tenant); err != ErrDenied {
		t.Fatal("admission revocation cached", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := registry.CheckActive(cancelled, tenant); err != context.Canceled {
		t.Fatal("admission cancellation", err)
	}
	var plan string
	if err := db.QueryRow("EXPLAIN ANALYZE SELECT status='active' FROM "+schema+".data_model_tenants WHERE id=?", mysqlstore.UUID(tenant)).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	t.Log(plan)
	// ANALYZE may fold a const-table lookup into "Rows fetched before
	// execution". The JSON plan retains the actual key used for that lookup.
	var jsonPlan string
	if err := db.QueryRow("EXPLAIN FORMAT=JSON SELECT status='active' FROM "+schema+".data_model_tenants WHERE id=?", mysqlstore.UUID(tenant)).Scan(&jsonPlan); err != nil {
		t.Fatal(err)
	}
	var access struct {
		QueryBlock struct {
			Table struct {
				Key        string `json:"key"`
				AccessType string `json:"access_type"`
			} `json:"table"`
		} `json:"query_block"`
	}
	if err := json.Unmarshal([]byte(jsonPlan), &access); err != nil {
		t.Fatal(err)
	}
	if access.QueryBlock.Table.Key != "PRIMARY" || access.QueryBlock.Table.AccessType != "const" {
		t.Fatal("admission point read lost its unique lookup", jsonPlan)
	}
	t.Log(jsonPlan)
	if err := db.QueryRow("EXPLAIN ANALYZE SELECT id,status='active' FROM "+schema+".data_model_tenants FORCE INDEX(PRIMARY) WHERE id>? ORDER BY id LIMIT 20", mysqlstore.UUID(uuid.Nil)).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan, "PRIMARY") || strings.Contains(strings.ToLower(plan), "table scan") {
		t.Fatal(plan)
	}
	t.Log(plan)
}
