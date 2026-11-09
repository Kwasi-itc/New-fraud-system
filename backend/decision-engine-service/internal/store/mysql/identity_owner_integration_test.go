package mysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/mysqlidentity"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"github.com/google/uuid"
)

func installIdentityOwner(t *testing.T, f platformFixture) migrations.Plan {
	t.Helper()
	ctx := context.Background()
	ref := platformTestDB(t)
	source, err := mysqlidentity.Statements(ref.database, "root")
	if err != nil {
		t.Fatal(err)
	}
	target, err := mysqlidentity.Statements(f.database, "root")
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
			t.Fatalf("identity %s: %v", s.ID, err)
		}
		def, err := migrations.Definition(ctx, conn, ref.database, s.Kind, s.Name)
		if err != nil {
			t.Fatal(err)
		}
		target[i].After = strings.ReplaceAll(def, ref.database, f.database)
	}
	plan, err := mysqlidentity.Plan(f.database, "root", target)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := migrations.NewForOwner(f.db, "identity", plan)
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
	return plan
}

func seedIdentity(t *testing.T, f platformFixture, tenant uuid.UUID, roles string) TrustedDecisionSession {
	t.Helper()
	ctx := context.Background()
	s, _ := mysqlstore.Identifier(f.database)
	session := TrustedDecisionSession{ActorID: uuid.New(), TenantID: tenant, SessionID: uuid.New(), MembershipVersion: 1}
	statements := []struct {
		q    string
		args []any
	}{
		{"INSERT INTO " + s + ".identity_tenants(id,active) VALUES(?,TRUE) ON DUPLICATE KEY UPDATE active=active", []any{mysqlstore.UUID(tenant)}},
		{"INSERT INTO " + s + ".identity_users(id,display_name,active) VALUES(?,'test',TRUE)", []any{mysqlstore.UUID(session.ActorID)}},
		{"INSERT INTO " + s + ".identity_memberships(tenant_id,user_id,roles,active) VALUES(?,?,?,TRUE)", []any{mysqlstore.UUID(tenant), mysqlstore.UUID(session.ActorID), roles}},
	}
	for _, st := range statements {
		if _, err := f.db.ExecContext(ctx, st.q, st.args...); err != nil {
			t.Fatal(err)
		}
	}
	digest := sha256.Sum256([]byte(session.SessionID.String()))
	if _, err := f.db.ExecContext(ctx, "INSERT INTO "+s+".identity_sessions(id,digest,tenant_id,user_id,created_at,expires_at,idle_until) VALUES(?,?,?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6)+INTERVAL 1 HOUR,UTC_TIMESTAMP(6)+INTERVAL 30 MINUTE)", mysqlstore.UUID(session.SessionID), digest[:], mysqlstore.UUID(tenant), mysqlstore.UUID(session.ActorID)); err != nil {
		t.Fatal(err)
	}
	return session
}

func TestMySQLIdentityOwnerRolesAndRelationships(t *testing.T) {
	f := platformTestDB(t)
	installIdentityOwner(t, f)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.database)
	actions := []AuthoringAction{AuthoringRead, AuthoringWrite, AuthoringPublish, AuthoringWorkflowRead, AuthoringWorkflowWrite, AuthoringCustomListRead, AuthoringCustomListWrite}
	cases := []struct {
		roles string
		allow string
	}{
		{"tenant_admin", ""}, {"case_manager_admin", ""}, {"case_investigator", ""},
		{"platform_viewer", "decision.read decision.list.read"},
		{"platform_builder", "decision.read decision.author decision.list.read decision.list.mutate"},
		{"platform_publisher", "decision.read decision.publish decision.list.read"},
		{"platform_admin", "decision.read decision.author decision.publish decision.list.read decision.list.mutate"},
		{"platform_admin,case_investigator", "decision.read decision.author decision.publish decision.workflow.read decision.workflow.author decision.list.read decision.list.mutate"},
		{"platform_builder,platform_publisher", "decision.read decision.author decision.publish decision.list.read decision.list.mutate"},
	}
	for _, tc := range cases {
		session := seedIdentity(t, f, uuid.New(), tc.roles)
		auth, err := SessionAuthorizer(f.database, session)
		if err != nil {
			t.Fatal(err)
		}
		for _, action := range actions {
			t.Run(tc.roles+"/"+string(action), func(t *testing.T) {
				tx, err := f.db.BeginTx(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				err = auth(ctx, tx, session.TenantID, action)
				want := strings.Contains(" "+tc.allow+" ", " "+string(action)+" ")
				if (err == nil) != want {
					t.Fatalf("allow=%v error=%v", want, err)
				}
			})
		}
	}
	session := seedIdentity(t, f, uuid.New(), "platform_admin,case_manager_admin")
	for _, tc := range []struct {
		name   string
		change func(*TrustedDecisionSession)
	}{
		{"wrong_tenant", func(s *TrustedDecisionSession) { s.TenantID = uuid.New() }}, {"wrong_actor", func(s *TrustedDecisionSession) { s.ActorID = uuid.New() }}, {"wrong_session", func(s *TrustedDecisionSession) { s.SessionID = uuid.New() }}, {"stale_membership", func(s *TrustedDecisionSession) { s.MembershipVersion = 2 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := session
			tc.change(&copy)
			auth, err := SessionAuthorizer(f.database, copy)
			if err != nil {
				t.Fatal(err)
			}
			tx, err := f.db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if err := auth(ctx, tx, copy.TenantID, AuthoringWrite); err == nil {
				t.Fatal("invalid session accepted")
			}
		})
	}
	t.Run("unknown_action_and_false_lock_flag", func(t *testing.T) {
		for _, action := range []string{"unknown", "decision.author"} {
			var allowed bool
			if err := f.db.QueryRowContext(ctx, "SELECT "+schema+".identity_authorize_decision_v1(?,?,?,?,?,FALSE)", mysqlstore.UUID(session.ActorID), mysqlstore.UUID(session.TenantID), mysqlstore.UUID(session.SessionID), 1, action).Scan(&allowed); err != nil || allowed {
				t.Fatal("unsafe action", allowed, err)
			}
		}
	})
	t.Run("storage_relationships", func(t *testing.T) {
		for _, st := range []string{
			"UPDATE " + schema + ".identity_memberships SET version=0 WHERE user_id=?",
			"UPDATE " + schema + ".identity_memberships SET active=FALSE WHERE user_id=?",
			"UPDATE " + schema + ".identity_memberships SET roles='' ,version=2 WHERE user_id=?",
			"UPDATE " + schema + ".identity_sessions SET tenant_id=RANDOM_BYTES(16) WHERE user_id=?",
			"UPDATE " + schema + ".identity_sessions SET expires_at=created_at WHERE user_id=?",
		} {
			if _, err := f.db.ExecContext(ctx, st, mysqlstore.UUID(session.ActorID)); err == nil {
				t.Fatal("invalid relationship accepted", st)
			}
		}
		if _, err := f.db.ExecContext(ctx, "INSERT INTO "+schema+".identity_memberships(tenant_id,user_id,roles,active) VALUES(?,?,'platform_admin',TRUE)", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(session.ActorID)); err == nil {
			t.Fatal("foreign tenant membership accepted")
		}
	})
	t.Run("read_only_session", func(t *testing.T) {
		if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".identity_sessions SET read_only=TRUE WHERE id=?", mysqlstore.UUID(session.SessionID)); err != nil {
			t.Fatal(err)
		}
		auth, _ := SessionAuthorizer(f.database, session)
		tx, err := f.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if err := auth(ctx, tx, session.TenantID, AuthoringWrite); err == nil {
			t.Fatal("read only write allowed")
		}
		if err := auth(ctx, tx, session.TenantID, AuthoringRead); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("session_insert_relationships", func(t *testing.T) {
		other := seedIdentity(t, f, uuid.New(), "platform_viewer")
		for _, foreign := range []bool{false, true} {
			digest := sha256.Sum256([]byte(session.SessionID.String()))
			tenant := session.TenantID
			if foreign {
				tenant = other.TenantID
				digest = sha256.Sum256([]byte(uuid.NewString()))
			}
			_, err := f.db.ExecContext(ctx, "INSERT INTO "+schema+".identity_sessions(id,digest,tenant_id,user_id,created_at,expires_at,idle_until) VALUES(?,?,?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6)+INTERVAL 1 HOUR,UTC_TIMESTAMP(6)+INTERVAL 30 MINUTE)", mysqlstore.UUID(uuid.New()), digest[:], mysqlstore.UUID(tenant), mysqlstore.UUID(session.ActorID))
			if err == nil {
				t.Fatal("duplicate digest or cross-tenant session accepted", foreign)
			}
		}
	})
	for _, state := range []string{"inactive_user", "inactive_tenant", "expired"} {
		t.Run(state, func(t *testing.T) {
			session := seedIdentity(t, f, uuid.New(), "platform_admin")
			var err error
			switch state {
			case "inactive_user":
				_, err = f.db.ExecContext(ctx, "UPDATE "+schema+".identity_users SET active=FALSE WHERE id=?", mysqlstore.UUID(session.ActorID))
			case "inactive_tenant":
				_, err = f.db.ExecContext(ctx, "UPDATE "+schema+".identity_tenants SET active=FALSE WHERE id=?", mysqlstore.UUID(session.TenantID))
			case "expired":
				_, err = f.db.ExecContext(ctx, "UPDATE "+schema+".identity_sessions SET expires_at=UTC_TIMESTAMP(6),idle_until=UTC_TIMESTAMP(6) WHERE id=?", mysqlstore.UUID(session.SessionID))
			}
			if err != nil {
				t.Fatal(err)
			}
			auth, _ := SessionAuthorizer(f.database, session)
			tx, err := f.db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if err := auth(ctx, tx, session.TenantID, AuthoringRead); err == nil {
				t.Fatal("inactive/expired identity authorized")
			}
		})
	}
}

func TestMySQLIdentityOwnerRechecksAfterWait(t *testing.T) {
	f := platformTestDB(t)
	installIdentityOwner(t, f)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.database)
	for _, change := range []string{"revocation", "expiry", "membership"} {
		t.Run(change, func(t *testing.T) {
			session := seedIdentity(t, f, uuid.New(), "platform_builder")
			hold, err := f.db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer hold.Rollback()
			var id mysqlstore.UUID
			if err := hold.QueryRowContext(ctx, "SELECT id FROM "+schema+".identity_users WHERE id=? FOR UPDATE", mysqlstore.UUID(session.ActorID)).Scan(&id); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "revocation":
				_, err = hold.ExecContext(ctx, "UPDATE "+schema+".identity_sessions SET revoked_at=UTC_TIMESTAMP(6) WHERE id=?", mysqlstore.UUID(session.SessionID))
			case "membership":
				_, err = hold.ExecContext(ctx, "UPDATE "+schema+".identity_memberships SET active=FALSE,version=version+1 WHERE tenant_id=? AND user_id=?", mysqlstore.UUID(session.TenantID), mysqlstore.UUID(session.ActorID))
			case "expiry":
				_, err = hold.ExecContext(ctx, "UPDATE "+schema+".identity_sessions SET idle_until=UTC_TIMESTAMP(6)+INTERVAL 1 SECOND WHERE id=?", mysqlstore.UUID(session.SessionID))
			}
			if err != nil {
				t.Fatal(err)
			}
			ready := make(chan struct{})
			done := make(chan error, 1)
			go func() {
				tx, e := f.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
				if e != nil {
					done <- e
					return
				}
				defer tx.Rollback()
				auth, e := SessionAuthorizer(f.database, session)
				if e != nil {
					done <- e
					return
				}
				close(ready)
				done <- auth(ctx, tx, session.TenantID, AuthoringWrite)
			}()
			<-ready
			// Observe the server lock wait, rather than assuming a goroutine has run.
			deadline := time.Now().Add(3 * time.Second)
			for {
				var waits int
				if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM performance_schema.data_lock_waits").Scan(&waits); err != nil {
					t.Fatal(err)
				}
				if waits > 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("authorization did not wait")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if change == "expiry" {
				time.Sleep(1100 * time.Millisecond)
			}
			if err := hold.Commit(); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("stale authority accepted after lock wait")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("authorization blocked")
			}
		})
	}
}
