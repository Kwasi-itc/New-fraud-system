package mysqlruntime

import (
	"context"
	"encoding/base64"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/mysqlidentity"
	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/sessioncredential"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/testsupport"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestMySQLVerifiedSessionComposition(t *testing.T) {
	db, database, _, _ := testsupport.DecisionMySQLRuntimeDatabase(t)
	testsupport.InstallDecisionAccessOwners(t, db, database)
	schema, _ := mysqlstore.Identifier(database)
	ctx := context.Background()
	tenant, actor, sid := uuid.New(), uuid.New(), uuid.New()
	raw := base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("a", 32)))
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO "+schema+".identity_tenants VALUES(?,TRUE)", mysqlstore.UUID(tenant))
	exec("INSERT INTO "+schema+".identity_users VALUES(?,'author',TRUE)", mysqlstore.UUID(actor))
	exec("INSERT INTO "+schema+".identity_memberships(tenant_id,user_id,roles,active) VALUES(?,?,'platform_admin,case_manager_admin',TRUE)", mysqlstore.UUID(tenant), mysqlstore.UUID(actor))
	exec("INSERT INTO "+schema+".identity_sessions(id,digest,tenant_id,user_id,created_at,expires_at,idle_until) VALUES(?,?,?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6)+INTERVAL 1 HOUR,UTC_TIMESTAMP(6)+INTERVAL 30 MINUTE)", mysqlstore.UUID(sid), sessioncredential.Digest(raw), mysqlstore.UUID(tenant), mysqlstore.UUID(actor))
	verifier, err := mysqlidentity.NewVerifier(db, database)
	if err != nil {
		t.Fatal(err)
	}
	deps, err := BindSessionIdentity(Dependencies{DB: db, Database: database}, verifier)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BindSessionIdentity(Dependencies{DB: db, Database: "wrong_schema"}, verifier); err == nil {
		t.Fatal("cross database verifier accepted")
	}
	for _, token := range []string{"", "forged", base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("b", 32)))} {
		req := httptest.NewRequest("GET", "/", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set("X-Workflow-User", actor.String())
		req.Header.Set("X-Workflow-Session", sid.String())
		if _, err := deps.Authenticate(req); err == nil {
			t.Fatal("forged credential accepted")
		}
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	req.Header.Set("X-Workflow-User", uuid.NewString())
	verified, err := deps.Authenticate(req)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := deps.RequestAuthority(verified, tenant)
	if err != nil {
		t.Fatal(err)
	}
	if authority.Session.ActorID != actor || authority.Session.SessionID != sid {
		t.Fatal("untrusted headers replaced verifier identity")
	}
	if _, err := deps.RequestAuthority(ctx, tenant); err == nil {
		t.Fatal("missing verified context accepted")
	}
	if _, err := deps.RequestAuthority(verified, uuid.New()); err == nil {
		t.Fatal("foreign tenant accepted")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.Authoring(ctx, tx, tenant, mysql.AuthoringWorkflowWrite); err != nil {
		t.Fatal(err)
	}
	inbox := uuid.New()
	exec("INSERT INTO "+schema+".case_manager_inboxes VALUES(?,?,'allowed',TRUE)", mysqlstore.UUID(inbox), mysqlstore.UUID(tenant))
	c, err := mysql.NewInboxContract(database, *authority.Session)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Authorize(ctx, tx, tenant, inbox, true, true); err != nil {
		t.Fatal(err)
	}
	if err := authority.Persist(ctx, tx, tenant, true); err == nil {
		t.Fatal("session inherited persistence")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	exec("UPDATE "+schema+".identity_sessions SET revoked_at=UTC_TIMESTAMP(6) WHERE id=?", mysqlstore.UUID(sid))
	if _, err := deps.Authenticate(req); err == nil {
		t.Fatal("revoked credential accepted")
	}
	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := authority.Authoring(ctx, tx, tenant, mysql.AuthoringWrite); err == nil {
		t.Fatal("cached verified context bypassed revocation")
	}
}
