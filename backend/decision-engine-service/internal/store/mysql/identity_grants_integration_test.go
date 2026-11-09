package mysql

import (
	"context"
	"database/sql"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/mysqlidentity"
	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/sessioncredential"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/mysqlaccess"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func restrictedIdentityPool(t *testing.T, f platformFixture, writeLists bool) *sql.DB {
	t.Helper()
	ctx := context.Background()
	user := "identity_api_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	quoted, _ := mysqlstore.Identifier(user)
	schema, _ := mysqlstore.Identifier(f.database)
	if _, err := f.db.ExecContext(ctx, "CREATE USER "+quoted+"@'%' IDENTIFIED BY 'isolated-test-only'"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.db.ExecContext(context.Background(), "DROP USER "+quoted+"@'%'"); err != nil {
			t.Error(err)
		}
	})
	grants, err := mysqlidentity.RuntimeGrants(f.database, user)
	if err != nil {
		t.Fatal(err)
	}
	cases, err := mysqlaccess.RuntimeGrants(f.database, user)
	if err != nil {
		t.Fatal(err)
	}
	grants = append(grants, cases...)
	verbs := "SELECT"
	if writeLists {
		verbs = ",INSERT,UPDATE,DELETE"
		verbs = "SELECT" + verbs
	}
	for _, table := range []string{"decision_engine_custom_lists", "decision_engine_custom_list_entries"} {
		grants = append(grants, "GRANT "+verbs+" ON "+schema+"."+table+" TO "+quoted+"@'%'")
	}
	// Name resolution is a read of decision's exact-key owner, not a grant to
	// create keys or mutate another owner's tables.
	grants = append(grants, "GRANT SELECT ON "+schema+".decision_engine_exact_keys TO "+quoted+"@'%'")
	for _, grant := range grants {
		if _, err := f.db.ExecContext(ctx, grant); err != nil {
			t.Fatal(grant, err)
		}
	}
	db, err := mysqlstore.Open(ctx, platformConfig(f.database, user, "isolated-test-only"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestMySQLIdentityAndListRestrictedGrants(t *testing.T) {
	f := scenarioFoundationDB(t, true, true, true, true, true, true)
	installIdentityOwner(t, f)
	installCaseOwner(t, f)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.database)
	session := seedIdentity(t, f, uuid.New(), "platform_admin,case_manager_admin")
	raw := base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("q", 32)))
	// Digest is immutable: create another actual owner session for verification.
	session.SessionID = uuid.New()
	if _, err := f.db.ExecContext(ctx, "INSERT INTO "+schema+".identity_sessions(id,digest,tenant_id,user_id,created_at,expires_at,idle_until) VALUES(?,?,?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6)+INTERVAL 1 HOUR,UTC_TIMESTAMP(6)+INTERVAL 30 MINUTE)", mysqlstore.UUID(session.SessionID), sessioncredential.Digest(raw), mysqlstore.UUID(session.TenantID), mysqlstore.UUID(session.ActorID)); err != nil {
		t.Fatal(err)
	}
	inbox := uuid.New()
	if _, err := f.db.ExecContext(ctx, "INSERT INTO "+schema+".case_manager_inboxes VALUES(?,?,'allowed',TRUE)", mysqlstore.UUID(inbox), mysqlstore.UUID(session.TenantID)); err != nil {
		t.Fatal(err)
	}
	writer := restrictedIdentityPool(t, f, true)
	reader := restrictedIdentityPool(t, f, false)
	for _, db := range []*sql.DB{writer, reader} {
		verifier, err := mysqlidentity.NewVerifier(db, f.database)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := verifier.Resolve(ctx, raw); err != nil || got.ActorID != session.ActorID {
			t.Fatal("restricted verifier", got, err)
		}
		auth, _ := SessionAuthorizer(f.database, session)
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := auth(ctx, tx, session.TenantID, AuthoringWorkflowWrite); err != nil {
			t.Fatal(err)
		}
		contract, _ := NewInboxContract(f.database, session)
		if err := contract.Authorize(ctx, tx, session.TenantID, inbox, true, true); err != nil {
			t.Fatal(err)
		}
		tx.Rollback()
		for _, table := range []string{"identity_users", "identity_memberships", "identity_sessions", "case_manager_inboxes", "case_manager_inbox_users", "case_manager_tags"} {
			if _, err := db.ExecContext(ctx, "DELETE FROM "+schema+"."+table+" WHERE FALSE"); err == nil {
				t.Fatal("foreign write granted", table)
			}
		}
	}
	auth, _ := SessionAuthorizer(f.database, session)
	store, err := NewCustomListStore(writer, f.database, session.TenantID, 500, 2<<20, auth)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	list := platform.CustomList{ID: uuid.NewString(), TenantID: session.TenantID.String(), Name: "Restricted list", Kind: "generic_text", CreatedAt: at, UpdatedAt: at}
	if err := store.Mutate(ctx, func(r CustomListRepository) error { _, err := r.CreateList(ctx, list); return err }); err != nil {
		t.Fatal(err)
	}
	read, err := NewCustomListStore(reader, f.database, session.TenantID, 500, 2<<20, auth)
	if err != nil {
		t.Fatal(err)
	}
	if err := read.ReadSnapshot(ctx, func(r CustomListRepository) error { _, err := r.GetListByID(ctx, list.TenantID, list.ID); return err }); err != nil {
		t.Fatal(err)
	}
	list.ID = uuid.NewString()
	list.Name = "Forbidden"
	if err := read.Mutate(ctx, func(r CustomListRepository) error { _, err := r.CreateList(ctx, list); return err }); err == nil {
		t.Fatal("read SQL account mutated a list")
	}
	viewer := seedIdentity(t, f, session.TenantID, "platform_viewer")
	viewAuth, _ := SessionAuthorizer(f.database, viewer)
	viewStore, err := NewCustomListStore(writer, f.database, session.TenantID, 500, 2<<20, viewAuth)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	if err := viewStore.Mutate(ctx, func(r CustomListRepository) error { called = true; return nil }); err == nil || called {
		t.Fatal("viewer inherited writer pool authority")
	}
}
