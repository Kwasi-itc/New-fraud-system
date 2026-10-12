package postgres_test

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"os"
	"testing"
)

func TestPlatformIdentityRevokesCaseAndOwnerCommands(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, inbox, c := fixture(t, db)
	ctx := context.Background()
	schema, err := os.ReadFile("../../../../auth-service/internal/platform/schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, string(schema)); err != nil {
		t.Fatal(err)
	}
	user, sid := uuid.New(), uuid.New()
	for _, seed := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO identity.users(id,display_name) VALUES($1,'Investigator')`, []any{user}},
		{`INSERT INTO identity.memberships(user_id,tenant_id,roles) VALUES($1,$2,ARRAY['case_manager_admin'])`, []any{user, tenant}},
		{`INSERT INTO identity.sessions(id,user_id,tenant_id,digest,expires_at,idle_until) VALUES($3,$1,$2,'test',now()+interval '1 hour',now()+interval '1 hour')`, []any{user, tenant, sid}},
	} {
		if _, err = db.Exec(ctx, seed.sql, seed.args...); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("USER_REQUIRE_SESSION", "true")
	principal := access.Principal{Kind: access.User, Subject: user.String(), TenantID: tenant, Admin: true, SessionID: sid, AuthzVersion: 1}
	userCtx := access.WithPrincipal(ctx, principal)
	if _, err = s.CreateComment(userCtx, tenant, c.ID, "Verified active session", nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PutInboxUser(userCtx, tenant, inbox.ID, "unprovisioned-user", true); err == nil {
		t.Fatal("arbitrary inbox subject accepted")
	}
	if _, err = db.Exec(ctx, `UPDATE identity.memberships SET active=false,version=version+1 WHERE user_id=$1`, user); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateComment(userCtx, tenant, c.ID, "Revoked", nil); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatal("case command not denied", err)
	}
	_, err = identity.Run(ctx, db, identity.Principal{Subject: user.String(), TenantID: tenant, Admin: true, SessionID: sid, AuthzVersion: 1}, tenant, c.ID, "resource", "core", nil, func(context.Context, pgx.Tx, *identity.Command) (identity.State, error) {
		t.Fatal("revoked owner action executed")
		return identity.State{}, nil
	})
	var denied *identity.Error
	if !errors.As(err, &denied) || denied.Code != 403 {
		t.Fatal("owner command not denied", err)
	}
}
