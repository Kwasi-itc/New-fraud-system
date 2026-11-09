package postgres

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func TestScopedIndexJobsPostgres(t *testing.T) {
	dsn := repositoryIntegrationDatabaseURL(t)
	ctx := context.Background()
	db := repositoryIntegrationPool(t, ctx, dsn)
	defer db.Close()
	resetRepositoryIntegrationDatabase(t, ctx, db, dsn)
	owner := uuid.New()
	a, b := uuid.New(), uuid.New()
	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := NewTenantRepository(db).Create(ctx, tenant.New(owner, at, "scope", nil)); err != nil {
		t.Fatal(err)
	}
	r := NewIndexJobRepository(db)
	for i, id := range []uuid.UUID{a, b} {
		name := []string{"a", "b"}[i]
		if err := NewTableRepository(db).Create(ctx, datamodel.Table{ID: id, TenantID: owner, Name: name, CreatedAt: at, UpdatedAt: at}); err != nil {
			t.Fatal(err)
		}
		for j := 0; j < 3; j++ {
			if err := r.Create(ctx, datamodel.IndexJob{ID: uuid.New(), TenantID: owner, TableID: &id, TableName: name, IndexType: "search", Columns: []string{}, Status: "pending", RequestedAt: at.Add(time.Duration(j) * time.Second)}); err != nil {
				t.Fatal(err)
			}
		}
	}
	jobs, err := r.ListByTables(ctx, owner, []uuid.UUID{a}, 3)
	if err != nil || len(jobs) != 3 {
		t.Fatal(jobs, err)
	}
	for _, job := range jobs {
		if job.TableID == nil || *job.TableID != a {
			t.Fatal("unrelated table leaked")
		}
	}
	if _, err := r.ListByTables(ctx, owner, []uuid.UUID{a}, 2); !errors.Is(err, datamodel.ErrIndexJobScopeLimit) {
		t.Fatal("partial overflow", err)
	}
	jobs, err = r.ListByTables(ctx, uuid.New(), []uuid.UUID{a}, 3)
	if err != nil || len(jobs) != 0 {
		t.Fatal("tenant leaked", jobs, err)
	}
	if _, err := r.ListByTables(ctx, owner, []uuid.UUID{a, a}, 3); err == nil {
		t.Fatal("duplicate tables accepted")
	}
	large := strings.Repeat("x", 70000)
	if _, err := db.Exec(ctx, `INSERT INTO core.index_jobs(id,tenant_id,table_id,table_name,index_type,columns,status,requested_by_operation,attempt_count,requested_at,dedupe_key)
	SELECT gen_random_uuid(),$1,$2,'b','search',ARRAY[]::text[],'pending','scope-plan-fixture',0,$3,'' FROM generate_series(1,5000)`, owner, b, at); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "ANALYZE core.index_jobs"); err != nil {
		t.Fatal(err)
	}
	var plan string
	if err := db.QueryRow(ctx, `EXPLAIN (FORMAT JSON) SELECT id FROM core.index_jobs WHERE tenant_id=$1 AND table_id=ANY($2::uuid[]) ORDER BY requested_at DESC,id DESC LIMIT 4`, owner, []uuid.UUID{a}).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan, "index_jobs_tenant_table_requested_idx") {
		t.Fatal("scoped index not selected", plan)
	}
	t.Log("unhinted scoped index selected with 5000 unrelated jobs")
	jobs, err = r.ListByTables(ctx, owner, []uuid.UUID{a}, 3)
	if err != nil || len(jobs) != 3 {
		t.Fatal("unrelated history consumed scope budget", len(jobs), err)
	}
	if err := r.Create(ctx, datamodel.IndexJob{ID: uuid.New(), TenantID: owner, TableID: &a, TableName: "a", IndexType: "search", Columns: []string{}, Status: "failed", RequestedAt: at, ErrorMessage: &large}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ListByTables(ctx, owner, []uuid.UUID{a}, 10); err == nil {
		t.Fatal("oversized projection accepted")
	}
}
