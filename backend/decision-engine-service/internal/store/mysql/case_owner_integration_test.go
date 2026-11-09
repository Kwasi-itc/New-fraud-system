package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/mysqlaccess"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"github.com/google/uuid"
)

func installCaseOwner(t *testing.T, f platformFixture) {
	t.Helper()
	ctx := context.Background()
	ref := platformTestDB(t)
	installIdentityOwner(t, ref)
	source, err := mysqlaccess.Statements(ref.database, "root")
	if err != nil {
		t.Fatal(err)
	}
	target, err := mysqlaccess.Statements(f.database, "root")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := ref.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for i, s := range source {
		if _, err := conn.ExecContext(ctx, s.SQL); err != nil {
			t.Fatalf("case %s: %v", s.ID, err)
		}
		def, err := migrations.Definition(ctx, conn, ref.database, s.Kind, s.Name)
		if err != nil {
			t.Fatal(err)
		}
		target[i].After = strings.ReplaceAll(def, ref.database, f.database)
	}
	plan, err := mysqlaccess.Plan(f.database, "root", target)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := migrations.NewForOwner(f.db, "case_manager", plan)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := runner.Apply(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := runner.Verify(ctx, nil); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLCaseOwnerBoundedContracts(t *testing.T) {
	f := platformTestDB(t)
	installIdentityOwner(t, f)
	installCaseOwner(t, f)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.database)
	session := seedIdentity(t, f, uuid.New(), "platform_admin,case_investigator")
	inbox, denied, inactive, tag, deleted := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, r := range []struct {
		table  string
		id     uuid.UUID
		active bool
	}{{"case_manager_inboxes", inbox, true}, {"case_manager_inboxes", denied, true}, {"case_manager_inboxes", inactive, false}, {"case_manager_tags", tag, true}, {"case_manager_tags", deleted, false}} {
		if _, err := f.db.ExecContext(ctx, "INSERT INTO "+schema+"."+r.table+"(id,tenant_id,name,active) VALUES(?,?,'test',?)", mysqlstore.UUID(r.id), mysqlstore.UUID(session.TenantID), r.active); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []uuid.UUID{inbox, inactive} {
		if _, err := f.db.ExecContext(ctx, "INSERT INTO "+schema+".case_manager_inbox_users(tenant_id,inbox_id,user_id) VALUES(?,?,?)", mysqlstore.UUID(session.TenantID), mysqlstore.UUID(id), mysqlstore.UUID(session.ActorID)); err != nil {
			t.Fatal(err)
		}
	}
	// Exercise public functions directly as well as decision consumers, ensuring
	// malformed input is denied even when a caller bypasses consumer validation.
	call := func(t *testing.T, name string, ids any, active, write bool) bool {
		t.Helper()
		raw, _ := json.Marshal(ids)
		tx, err := f.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		var allowed bool
		if err := tx.QueryRowContext(ctx, "SELECT "+schema+"."+name+"(?,?,?,?,?,?,?)", mysqlstore.UUID(session.ActorID), mysqlstore.UUID(session.TenantID), mysqlstore.UUID(session.SessionID), session.MembershipVersion, mysqlstore.JSON(raw), active, write).Scan(&allowed); err != nil {
			t.Fatal(err)
		}
		return allowed
	}
	for _, tc := range []struct {
		name, kind   string
		ids          []string
		active, want bool
	}{
		{"inbox_allowed", "inboxes", []string{inbox.String()}, true, true},
		{"mixed_denied", "inboxes", []string{denied.String(), inbox.String()}, true, false},
		{"inactive_denied", "inboxes", []string{inactive.String()}, true, false},
		{"inactive_historical", "inboxes", []string{inactive.String()}, false, true},
		{"missing", "inboxes", []string{uuid.NewString()}, true, false},
		{"duplicate", "inboxes", []string{inbox.String(), inbox.String()}, true, false},
		{"malformed", "inboxes", []string{"not-an-id"}, true, false},
		{"tag_allowed", "tags", []string{tag.String()}, true, true},
		{"tag_inactive", "tags", []string{deleted.String()}, true, false},
		{"tag_historical", "tags", []string{deleted.String()}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, write := range []bool{false, true} {
				if got := call(t, "case_manager_authorize_decision_"+tc.kind+"_v1", tc.ids, tc.active, write); got != tc.want {
					t.Fatalf("write=%v allowed=%v want=%v", write, got, tc.want)
				}
			}
		})
	}
	t.Run("batch_overflow", func(t *testing.T) {
		for kind, limit := range map[string]int{"inboxes": 500, "tags": 25} {
			ids := make([]string, limit+1)
			for i := range ids {
				ids[i] = uuid.NewString()
			}
			if call(t, "case_manager_authorize_decision_"+kind+"_v1", ids, true, true) {
				t.Fatal("overflow accepted")
			}
		}
	})
	t.Run("visibility_requested_only", func(t *testing.T) {
		raw, _ := json.Marshal([]string{denied.String(), inbox.String(), uuid.NewString()})
		var result []byte
		if err := f.db.QueryRowContext(ctx, "SELECT "+schema+".case_manager_visible_decision_inboxes_v1(?,?,?,?,?)", mysqlstore.UUID(session.ActorID), mysqlstore.UUID(session.TenantID), mysqlstore.UUID(session.SessionID), 1, mysqlstore.JSON(raw)).Scan(&result); err != nil {
			t.Fatal(err)
		}
		var ids []string
		if err := json.Unmarshal(result, &ids); err != nil || len(ids) != 1 || ids[0] != inbox.String() {
			t.Fatal("visibility", string(result), err)
		}
	})
	t.Run("single_inbox_consumer", func(t *testing.T) {
		c, err := NewInboxContract(f.database, session)
		if err != nil {
			t.Fatal(err)
		}
		tx, err := f.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if err := c.Authorize(ctx, tx, session.TenantID, inbox, true, true); err != nil {
			t.Fatal(err)
		}
		if err := c.Authorize(ctx, tx, session.TenantID, denied, true, true); err == nil {
			t.Fatal("unassigned inbox accepted")
		}
	})
	t.Run("maximum_batch_and_read_only_snapshot", func(t *testing.T) {
		ids := make([]uuid.UUID, 500)
		for i := range ids {
			ids[i] = uuid.New()
			if _, err := f.db.ExecContext(ctx, "INSERT INTO "+schema+".case_manager_inboxes VALUES(?,?,'batch',TRUE)", mysqlstore.UUID(ids[i]), mysqlstore.UUID(session.TenantID)); err != nil {
				t.Fatal(err)
			}
			if _, err := f.db.ExecContext(ctx, "INSERT INTO "+schema+".case_manager_inbox_users VALUES(?,?,?)", mysqlstore.UUID(session.TenantID), mysqlstore.UUID(ids[i]), mysqlstore.UUID(session.ActorID)); err != nil {
				t.Fatal(err)
			}
		}
		contract, _ := NewInboxContract(f.database, session)
		for _, write := range []bool{false, true} {
			tx, err := f.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: !write})
			if err != nil {
				t.Fatal(err)
			}
			if err := contract.AuthorizeBatch(ctx, tx, session.TenantID, ids, write, true); err != nil {
				tx.Rollback()
				t.Fatal(err)
			}
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			for i, j := 0, len(ids)-1; i < j; i, j = i+1, j-1 {
				ids[i], ids[j] = ids[j], ids[i]
			}
		}
	})
	t.Run("requested_row_locks_leave_other_inboxes_writable", func(t *testing.T) {
		bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		hold, err := f.db.BeginTx(bounded, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer hold.Rollback()
		contract, _ := NewInboxContract(f.database, session)
		if err := contract.AuthorizeBatch(bounded, hold, session.TenantID, []uuid.UUID{inbox}, true, true); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() {
			_, e := f.db.ExecContext(bounded, "UPDATE "+schema+".case_manager_inboxes SET name='changed' WHERE tenant_id=? AND id=?", mysqlstore.UUID(session.TenantID), mysqlstore.UUID(inbox))
			done <- e
		}()
		for {
			var n int
			if err := f.db.QueryRowContext(bounded, "SELECT COUNT(*) FROM performance_schema.data_lock_waits w JOIN performance_schema.data_locks l ON w.REQUESTING_ENGINE_LOCK_ID=l.ENGINE_LOCK_ID WHERE l.OBJECT_SCHEMA=? AND l.OBJECT_NAME='case_manager_inboxes'", f.database).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n > 0 {
				break
			}
			select {
			case e := <-done:
				t.Fatal("requested inbox was not locked", e)
			case <-bounded.Done():
				t.Fatal(bounded.Err())
			case <-time.After(10 * time.Millisecond):
			}
		}
		if _, err := f.db.ExecContext(bounded, "UPDATE "+schema+".case_manager_inboxes SET name='independent' WHERE tenant_id=? AND id=?", mysqlstore.UUID(session.TenantID), mysqlstore.UUID(denied)); err != nil {
			t.Fatal("unrelated inbox blocked", err)
		}
		if err := hold.Rollback(); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
	t.Run("case_admin_membership_override", func(t *testing.T) {
		if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".identity_memberships SET roles='platform_admin,case_manager_admin',version=version+1 WHERE tenant_id=? AND user_id=?", mysqlstore.UUID(session.TenantID), mysqlstore.UUID(session.ActorID)); err != nil {
			t.Fatal(err)
		}
		session.MembershipVersion++
		if !call(t, "case_manager_authorize_decision_inboxes_v1", []string{denied.String()}, true, true) {
			t.Fatal("case admin denied")
		}
	})
	t.Run("tenant_consistent_membership", func(t *testing.T) {
		if _, err := f.db.ExecContext(ctx, "INSERT INTO "+schema+".case_manager_inbox_users(tenant_id,inbox_id,user_id) VALUES(?,?,?)", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(inbox), mysqlstore.UUID(session.ActorID)); err == nil {
			t.Fatal("foreign membership accepted")
		}
	})
	t.Run("revocation_denies", func(t *testing.T) {
		if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".identity_sessions SET revoked_at=UTC_TIMESTAMP(6) WHERE id=?", mysqlstore.UUID(session.SessionID)); err != nil {
			t.Fatal(err)
		}
		if call(t, "case_manager_authorize_decision_inboxes_v1", []string{inbox.String()}, true, true) {
			t.Fatal("revoked access accepted")
		}
	})
}
