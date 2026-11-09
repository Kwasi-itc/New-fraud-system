package metadata_test

import (
	"context"
	"database/sql"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestScopedIndexJobs(t *testing.T) {
	db, database := metadataDB(t)
	ctx := context.Background()
	owner := uuid.New()
	a, b := uuid.New(), uuid.New()
	at := time.Now().UTC().Truncate(time.Microsecond)
	err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
		r, err := store.NewMetadataRepositories(tx, database, owner, 100, 1<<20)
		if err != nil {
			return err
		}
		if err := r.Tenants.Create(ctx, tenant.New(owner, at, "scope", nil)); err != nil {
			return err
		}
		for i, id := range []uuid.UUID{a, b} {
			name := []string{"a", "b"}[i]
			if err := r.Tables.Create(ctx, datamodel.Table{ID: id, TenantID: owner, Name: name, CreatedAt: at, UpdatedAt: at}); err != nil {
				return err
			}
			for j := 0; j < 3; j++ {
				if err := r.Jobs.Create(ctx, datamodel.IndexJob{ID: uuid.New(), TenantID: owner, TableID: &id, TableName: name, IndexType: "search", Columns: []string{}, Status: "pending", RequestedAt: at.Add(time.Duration(j) * time.Second)}); err != nil {
					return err
				}
			}
		}
		jobs, err := r.Jobs.ListByTables(ctx, owner, []uuid.UUID{a}, 3)
		if err != nil || len(jobs) != 3 {
			t.Fatalf("scope %d %v", len(jobs), err)
		}
		for _, job := range jobs {
			if job.TableID == nil || *job.TableID != a {
				t.Fatal("unrelated table leaked")
			}
		}
		if _, err := r.Jobs.ListByTables(ctx, owner, []uuid.UUID{a}, 2); !errors.Is(err, datamodel.ErrIndexJobScopeLimit) {
			t.Fatal("overflow returned partial success", err)
		}
		if _, err := r.Jobs.ListByTables(ctx, uuid.New(), []uuid.UUID{a}, 3); err == nil {
			t.Fatal("foreign tenant accepted")
		}
		if _, err := r.Jobs.ListByTables(ctx, owner, []uuid.UUID{a, a}, 3); err == nil {
			t.Fatal("duplicate table accepted")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
