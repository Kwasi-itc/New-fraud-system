package mysql

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

// SQL contract fixture only. Execution is queued for the user's database window;
// it does not implement or activate the real data-model/auth providers.
func TestMySQLAggregatePublicationRegistryFencingAndRollback(t *testing.T) {
	f := tenantRegistryFixture(t)
	ctx := context.Background()
	snapshot, _, _ := aggregatePublicationFixture(t)
	snapshot.Scenario.TenantID, snapshot.Iteration.TenantID, snapshot.Rules[0].TenantID = f.tenant.String(), f.tenant.String(), f.tenant.String()
	schema, _ := mysqlstore.Identifier(f.f.database)
	if _, err := f.f.db.ExecContext(ctx, "CREATE TABLE "+schema+".fixture_publication_marker(id INT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("required preparation failed")
	called := 0
	guard, err := f.reader.RegistryAggregatePublicationGuard(f.model, func(ctx context.Context, tx *sql.Tx, _ PublicationSnapshot) error {
		called++
		if _, err := tx.ExecContext(ctx, "INSERT INTO "+schema+".fixture_publication_marker VALUES(1)"); err != nil {
			return err
		}
		return rollback
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mysqlstore.Mutation(ctx, f.f.db, func(tx *sql.Tx) error { return guard(ctx, tx, snapshot) }); !errors.Is(err, rollback) || called != 1 {
		t.Fatal("guard/caller rollback", called, err)
	}
	var count int
	if err := f.f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".fixture_publication_marker").Scan(&count); err != nil || count != 0 {
		t.Fatal("preparation escaped transaction", count, err)
	}
	if _, err := f.f.db.ExecContext(ctx, "UPDATE "+schema+".data_model_physical_tables SET write_state='closed' WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(f.table)); err != nil {
		t.Fatal(err)
	}
	if err := mysqlstore.Mutation(ctx, f.f.db, func(tx *sql.Tx) error { return guard(ctx, tx, snapshot) }); !errors.Is(err, ErrPublishedModelUnavailable) || called != 1 {
		t.Fatal("closed registry accepted", called, err)
	}
	if _, err := f.f.db.ExecContext(ctx, "UPDATE "+schema+".data_model_physical_tables SET write_state='open' WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(f.table)); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	guard, err = f.reader.RegistryAggregatePublicationGuard(f.model, func(ctx context.Context, _ *sql.Tx, _ PublicationSnapshot) error {
		close(entered)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- mysqlstore.Mutation(ctx, f.f.db, func(tx *sql.Tx) error { return guard(ctx, tx, snapshot) })
	}()
	select {
	case <-entered:
	case err := <-done:
		t.Fatal("guard did not pin registry", err)
	case <-time.After(5 * time.Second):
		t.Fatal("guard entry timeout")
	}
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	wait, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	_, err = f.f.db.ExecContext(wait, "UPDATE "+schema+".data_model_physical_tables SET write_state='closed' WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(f.table))
	cancel()
	if err == nil {
		t.Fatal("schema barrier changed before publication preparation completed")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
