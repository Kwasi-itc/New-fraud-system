package mysqlauth

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/sessioncredential"
	"github.com/google/uuid"
)

const oldLocalIssuer = "http://localhost:8090/realms/fraud"
const newLocalIssuer = "http://localhost:8088/identity/realms/fraud"

func TestLocalIssuerValidation(t *testing.T) {
	for _, value := range []string{oldLocalIssuer, newLocalIssuer, "http://localhost:8099/identity/realms/fraud"} {
		if !localIssuer(value) {
			t.Fatal(value)
		}
	}
	for _, value := range []string{"http://remote.invalid:8088/identity/realms/fraud", "http://localhost/realms/fraud", "http://localhost:8088/identity/realms/master", newLocalIssuer + "?x=1", "http://user@localhost:8088/realms/fraud"} {
		if localIssuer(value) {
			t.Fatal(value)
		}
	}
}

func TestIssuerMovePreservesMembershipSessionAndInvitation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	user, err := f.store.Provision(ctx, f.tenant, oldLocalIssuer, "retained-subject", "Retained", []string{"tenant_admin", "platform_admin"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := f.store.CreateSessionForTenant(ctx, oldLocalIssuer, "retained-subject", f.tenant)
	if err != nil {
		t.Fatal(err)
	}
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	invites, err := NewInvitations(f.store, oldLocalIssuer, "http://localhost:8088", key)
	if err != nil {
		t.Fatal(err)
	}
	invite := uuid.New()
	if _, err = invites.Create(ctx, raw, invite, "pending@example.test", []string{"platform_viewer"}); err != nil {
		t.Fatal(err)
	}
	var payload []byte
	if err = f.db.QueryRow("SELECT payload FROM "+f.store.table("identity_invitation_delivery")+" WHERE invitation_id=?", id(invite)).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	login, err := f.store.BeginLogin(ctx, oldLocalIssuer)
	if err != nil {
		t.Fatal(err)
	}
	moved, err := f.store.MoveLocalIssuer(ctx, oldLocalIssuer, newLocalIssuer)
	if err != nil {
		t.Fatal(err)
	}
	if moved.Identities != 1 || moved.Invitations != 1 || moved.ExpiredLogins != 1 {
		t.Fatal(moved)
	}
	var identityUser string
	if err = f.db.QueryRow("SELECT HEX(user_id) FROM "+f.store.table("identity_external_identities")+" WHERE issuer=?", newLocalIssuer).Scan(&identityUser); err != nil || identityUser == "" {
		t.Fatal(err)
	}
	newSession, err := f.store.CreateSessionForTenant(ctx, newLocalIssuer, "retained-subject", f.tenant)
	if err != nil {
		t.Fatal(err)
	}
	for _, session := range []string{raw, newSession} {
		value, err := f.store.Resolve(ctx, session)
		if err != nil || value.UserID != user || len(value.Roles) != 2 {
			t.Fatal(value, err)
		}
	}
	if _, err = f.store.CreateSessionForTenant(ctx, oldLocalIssuer, "retained-subject", f.tenant); !errors.Is(err, denied) {
		t.Fatal("old issuer must not resolve", err)
	}
	if _, err = f.store.ConsumeLogin(ctx, login.State, login.Binding, newLocalIssuer); !errors.Is(err, denied) {
		t.Fatal("old login must expire", err)
	}
	newInvites, err := NewInvitations(f.store, newLocalIssuer, "http://localhost:8088", key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = newInvites.Envelope.Open(invite, payload); err != nil {
		t.Fatal("encrypted delivery material must survive", err)
	}
	var storedIssuer string
	if err = f.db.QueryRow("SELECT issuer FROM "+f.store.table("identity_invitations")+" WHERE id=?", id(invite)).Scan(&storedIssuer); err != nil || storedIssuer != newLocalIssuer {
		t.Fatal(storedIssuer, err)
	}
	var count int
	if err = f.db.QueryRow("SELECT COUNT(*) FROM "+f.store.table("identity_login_transactions")+" WHERE digest=?", sessioncredential.Digest(login.State)).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	again, err := f.store.MoveLocalIssuer(ctx, oldLocalIssuer, newLocalIssuer)
	if err != nil || again != (IssuerMoveResult{}) {
		t.Fatal("idempotent retry", again, err)
	}
}

func TestIssuerMoveCollisionRollsBackEntireMove(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for _, issuer := range []string{oldLocalIssuer, newLocalIssuer} {
		if _, err := f.store.Provision(ctx, f.tenant, issuer, "same-subject", "Independent", []string{"platform_viewer"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.store.MoveLocalIssuer(ctx, oldLocalIssuer, newLocalIssuer); !errors.Is(err, conflict) {
		t.Fatal("ambiguous identity must fail closed", err)
	}
	var count int
	if err := f.db.QueryRow("SELECT COUNT(*) FROM "+f.store.table("identity_external_identities")+" WHERE issuer=?", oldLocalIssuer).Scan(&count); err != nil || count != 1 {
		t.Fatal("rollback retained source", count, err)
	}
}
