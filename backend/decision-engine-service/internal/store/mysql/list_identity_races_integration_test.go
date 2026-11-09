package mysql

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
	"time"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestMySQLListRealIdentitySnapshotAndRevocation(t *testing.T) {
	f, admin, tenant := listAdminFixture(t)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.database)
	session := seedIdentity(t, f, tenant, "platform_viewer")
	authorize, err := SessionAuthorizer(f.database, session)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := NewCustomListStore(f.db, f.database, tenant, 500, 2<<20, authorize)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"rename", "delete", "update"} {
		t.Run(change, func(t *testing.T) {
			list, err := admin.Save(ctx, uuid.Nil, adminList("snapshot-"+change))
			if err != nil {
				t.Fatal(err)
			}
			id := uuid.MustParse(list.ID)
			if _, err := admin.Import(ctx, id, []string{"old"}); err != nil {
				t.Fatal(err)
			}
			node := domainast.Node{Function: "custom_list_access", NamedChildren: map[string]domainast.Node{"customListId": {Constant: list.ID}}}
			ready, release := make(chan struct{}), make(chan struct{})
			done := make(chan error, 1)
			go func() {
				done <- reader.ReadSnapshot(ctx, func(r CustomListRepository) error {
					first, e := ast_eval.EvaluateNode(ctx, node, ast_eval.Runtime{TenantID: tenant.String(), CustomListRepo: r})
					if e != nil {
						return e
					}
					close(ready)
					<-release
					second, e := ast_eval.EvaluateNode(ctx, node, ast_eval.Runtime{TenantID: tenant.String(), CustomListRepo: r})
					if e == nil && !reflect.DeepEqual(first, second) {
						t.Error("read snapshot changed during mutation")
					}
					return e
				})
			}()
			select {
			case <-ready:
			case e := <-done:
				t.Fatal(e)
			case <-time.After(5 * time.Second):
				t.Fatal("reader did not start")
			}
			switch change {
			case "rename":
				_, err = admin.Save(ctx, id, adminList("renamed-"+change))
			case "delete":
				err = admin.Delete(ctx, id)
			case "update":
				_, err = admin.Import(ctx, id, []string{"new"})
			}
			close(release)
			if err != nil {
				t.Fatal(err)
			}
			if e := <-done; e != nil {
				t.Fatal(e)
			}
		})
	}
	t.Run("revoked_during_snapshot", func(t *testing.T) {
		ready, release := make(chan struct{}), make(chan struct{})
		done := make(chan error, 1)
		go func() {
			done <- reader.ReadSnapshot(ctx, func(r CustomListRepository) error {
				close(ready)
				<-release
				_, e := r.ListLists(ctx, tenant.String())
				return e
			})
		}()
		<-ready
		if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".identity_sessions SET revoked_at=UTC_TIMESTAMP(6) WHERE id=?", mysqlstore.UUID(session.SessionID)); err != nil {
			close(release)
			t.Fatal(err)
		}
		close(release)
		if err := <-done; err != nil {
			t.Fatal("authorized snapshot should complete", err)
		}
		called := false
		if err := reader.ReadSnapshot(ctx, func(r CustomListRepository) error { called = true; return nil }); err == nil || called {
			t.Fatal("new snapshot accepted revoked identity")
		}
	})
	t.Run("write_waits_for_current_membership", func(t *testing.T) {
		builder := seedIdentity(t, f, tenant, "platform_builder")
		auth, _ := SessionAuthorizer(f.database, builder)
		writer, err := NewCustomListStore(f.db, f.database, tenant, 500, 2<<20, auth)
		if err != nil {
			t.Fatal(err)
		}
		hold, err := f.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer hold.Rollback()
		if _, err := hold.ExecContext(ctx, "UPDATE "+schema+".identity_memberships SET roles='platform_viewer',version=version+1 WHERE tenant_id=? AND user_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(builder.ActorID)); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		called := false
		go func() { done <- writer.Mutate(ctx, func(r CustomListRepository) error { called = true; return nil }) }()
		deadline := time.Now().Add(5 * time.Second)
		for {
			var n int
			if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM performance_schema.data_lock_waits").Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n > 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("no membership wait")
			}
			time.Sleep(10 * time.Millisecond)
		}
		if err := hold.Commit(); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err == nil || called {
			t.Fatal("stale membership entered list mutation")
		}
	})
}
