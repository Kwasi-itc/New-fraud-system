package platform

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/internal/oidcprovider"
	"github.com/google/uuid"
)

func invitationFixture(t *testing.T) (*InvitationService, uuid.UUID, string) {
	t.Helper()
	s, tenant := fixture(t)
	_, raw := provision(t, s, tenant, "inviter", "tenant_admin")
	i, err := NewInvitations(s, "https://provider.test", "https://app.test", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	return i, tenant, raw
}
func invitationToken(t *testing.T, i *InvitationService, id uuid.UUID) string {
	t.Helper()
	var b []byte
	if err := i.Store.DB.QueryRow(context.Background(), `SELECT payload FROM identity.invitation_delivery WHERE invitation_id=$1`, id).Scan(&b); err != nil {
		t.Fatal(err)
	}
	p, err := i.open(id, b)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte(p.Token)) {
		t.Fatal("plaintext delivery token")
	}
	return p.Token
}
func createInvite(t *testing.T, i *InvitationService, raw, email string) uuid.UUID {
	t.Helper()
	id, err := i.Create(context.Background(), raw, uuid.New(), email, []string{"case_investigator"})
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func recipient(email, subject string) oidcprovider.Identity {
	return oidcprovider.Identity{Issuer: "https://provider.test", Subject: subject, Email: email, EmailVerified: true, Name: "Recipient"}
}

func TestInvitationAcceptAtomicReplayAndIdentity(t *testing.T) {
	i, tenant, raw := invitationFixture(t)
	ctx := context.Background()
	id := createInvite(t, i, raw, "User@example.test")
	token := invitationToken(t, i, id)
	if _, err := i.Create(ctx, raw, id, "User@example.test", []string{"case_investigator"}); err != nil {
		t.Fatal("idempotency", err)
	}
	if _, err := i.Create(ctx, raw, uuid.New(), "User@example.test", []string{"case_investigator"}); !errors.Is(err, ErrConflict) {
		t.Fatal("duplicate pending", err)
	}
	for _, bad := range []oidcprovider.Identity{recipient("other@example.test", "other"), recipient("user@example.test", "casechanged"), {Issuer: "https://provider.test", Subject: "unverified", Email: "User@example.test"}, {Issuer: "https://evil.test", Subject: "evil", Email: "User@example.test", EmailVerified: true}} {
		if _, err := i.Accept(ctx, digest(token), bad); !errors.Is(err, ErrDenied) {
			t.Fatal("invalid recipient accepted", err)
		}
	}
	l, err := i.Begin(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = i.Store.ConsumeLogin(ctx, l.State, secret(), i.Issuer); !errors.Is(err, ErrDenied) {
		t.Fatal("browser binding", err)
	}
	consumed, err := i.Store.ConsumeLogin(ctx, l.State, l.Binding, i.Issuer)
	if err != nil || !bytes.Equal(consumed.InvitationDigest, digest(token)) {
		t.Fatal("login binding", err)
	}
	var wg sync.WaitGroup
	results := make(chan string, 2)
	errs := make(chan error, 2)
	for n := 0; n < 2; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, e := i.Accept(ctx, consumed.InvitationDigest, recipient("User@EXAMPLE.TEST", "recipient"))
			results <- s
			errs <- e
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	successes := 0
	for err := range errs {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrDenied) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("accepted %d times", successes)
	}
	for result := range results {
		if result != "" {
			s, e := i.Store.Resolve(ctx, result)
			if e != nil || s.TenantID != tenant || len(s.Roles) != 1 || s.Roles[0] != "case_investigator" {
				t.Fatal("wrong session", e)
			}
		}
	}
	var count int
	if err = i.Store.DB.QueryRow(ctx, `SELECT count(*) FROM identity.audit_events WHERE action='invitation.accepted' AND target_id=$1`, id).Scan(&count); err != nil || count != 1 {
		t.Fatal("audit", err, count)
	}
	var payload []byte
	if err = i.Store.DB.QueryRow(ctx, `SELECT payload FROM identity.invitation_delivery WHERE invitation_id=$1`, id).Scan(&payload); err != nil || payload != nil {
		t.Fatal("delivery secret retained", err)
	}
}
func TestInvitationCancelResendExpiryAndInviterRevocation(t *testing.T) {
	i, _, raw := invitationFixture(t)
	ctx := context.Background()
	id := createInvite(t, i, raw, "a@example.test")
	old := invitationToken(t, i, id)
	if _, err := i.Store.DB.Exec(ctx, `UPDATE identity.invitation_delivery SET requested_at=now()-interval '2 minutes' WHERE invitation_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err := i.Change(ctx, raw, id, 1, "resend"); err != nil {
		t.Fatal(err)
	}
	if _, err := i.Begin(ctx, old); !errors.Is(err, ErrDenied) {
		t.Fatal("old link", err)
	}
	if err := i.Change(ctx, raw, id, 1, "cancel"); !errors.Is(err, ErrConflict) {
		t.Fatal("stale update", err)
	}
	current := invitationToken(t, i, id)
	if err := i.Change(ctx, raw, id, 2, "cancel"); err != nil {
		t.Fatal(err)
	}
	if _, err := i.Accept(ctx, digest(current), recipient("a@example.test", "a")); !errors.Is(err, ErrDenied) {
		t.Fatal("cancel", err)
	}
	expired := createInvite(t, i, raw, "expired@example.test")
	token := invitationToken(t, i, expired)
	if _, err := i.Store.DB.Exec(ctx, `UPDATE identity.invitations SET expires_at=now()-interval '1 second' WHERE id=$1`, expired); err != nil {
		t.Fatal(err)
	}
	if _, err := i.Accept(ctx, digest(token), recipient("expired@example.test", "expired")); !errors.Is(err, ErrDenied) {
		t.Fatal("expiry", err)
	}
	stale := createInvite(t, i, raw, "stale@example.test")
	token = invitationToken(t, i, stale)
	c, err := i.Store.Resolve(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	if err = i.Store.ChangeMember(ctx, raw, c.UserID, c.Version, []string{"tenant_admin", "platform_viewer"}, true); err != nil {
		t.Fatal(err)
	}
	if _, err = i.Accept(ctx, digest(token), recipient("stale@example.test", "stale")); !errors.Is(err, ErrDenied) {
		t.Fatal("changed inviter", err)
	}
}
func TestInvitationExistingMembershipAndAuditRollback(t *testing.T) {
	i, tenant, raw := invitationFixture(t)
	ctx := context.Background()
	user, _ := provision(t, i.Store, tenant, "existing", "platform_viewer")
	id := createInvite(t, i, raw, "existing@example.test")
	token := invitationToken(t, i, id)
	if _, err := i.Accept(ctx, digest(token), recipient("existing@example.test", "existing")); !errors.Is(err, ErrConflict) {
		t.Fatal("membership overwritten", err)
	}
	var roles []string
	if err := i.Store.DB.QueryRow(ctx, `SELECT roles FROM identity.memberships WHERE tenant_id=$1 AND user_id=$2`, tenant, user).Scan(&roles); err != nil || len(roles) != 1 || roles[0] != "platform_viewer" {
		t.Fatal("roles changed", err)
	}
	id = createInvite(t, i, raw, "rollback@example.test")
	token = invitationToken(t, i, id)
	if _, err := i.Store.DB.Exec(ctx, `CREATE FUNCTION identity.reject_invite_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='invitation.accepted' THEN RAISE EXCEPTION 'test audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_invite_audit BEFORE INSERT ON identity.audit_events FOR EACH ROW EXECUTE FUNCTION identity.reject_invite_audit()`); err != nil {
		t.Fatal(err)
	}
	if _, err := i.Accept(ctx, digest(token), recipient("rollback@example.test", "rollback")); err == nil {
		t.Fatal("audit failure committed")
	}
	var count int
	if err := i.Store.DB.QueryRow(ctx, `SELECT count(*) FROM identity.external_identities WHERE subject='rollback'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("identity escaped rollback", err)
	}
	var status string
	if err := i.Store.DB.QueryRow(ctx, `SELECT status FROM identity.invitations WHERE id=$1`, id).Scan(&status); err != nil || status != "pending" {
		t.Fatal("consumed on rollback", err)
	}
}

type captureMail struct {
	calls int
	fail  bool
	link  string
}

func (m *captureMail) Send(_ context.Context, _ string, link, _ string) error {
	m.calls++
	m.link = link
	if m.fail {
		return errors.New("outage")
	}
	return nil
}
func TestInvitationDeliveryRetryAndIsolation(t *testing.T) {
	i, tenant, raw := invitationFixture(t)
	ctx := context.Background()
	id := createInvite(t, i, raw, "mail@example.test")
	token := invitationToken(t, i, id)
	_, viewer := provision(t, i.Store, tenant, "viewer", "platform_viewer")
	if _, err := i.Create(ctx, viewer, uuid.New(), "denied@example.test", []string{"tenant_admin"}); !errors.Is(err, ErrDenied) {
		t.Fatal("non admin create", err)
	}
	if _, err := i.List(ctx, viewer, uuid.Nil); !errors.Is(err, ErrDenied) {
		t.Fatal("non admin list", err)
	}
	other := uuid.New()
	if _, err := i.Store.DB.Exec(ctx, `INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'Other',$2,'active',now(),now())`, other, "tenant_"+strings.ReplaceAll(other.String(), "-", "")); err != nil {
		t.Fatal(err)
	}
	_, otherRaw := provision(t, i.Store, other, "other-admin", "tenant_admin")
	if err := i.Change(ctx, otherRaw, id, 1, "cancel"); !errors.Is(err, ErrDenied) {
		t.Fatal("foreign cancel", err)
	}
	listed, err := i.List(ctx, otherRaw, uuid.Nil)
	if err != nil || len(listed) != 0 {
		t.Fatal("foreign list", err)
	}
	m := &captureMail{fail: true}
	if done, err := i.DeliverOne(ctx, m); err != nil || !done {
		t.Fatal(err)
	}
	var state string
	if err := i.Store.DB.QueryRow(ctx, `SELECT state FROM identity.invitation_delivery WHERE invitation_id=$1`, id).Scan(&state); err != nil || state != "queued" {
		t.Fatal("retry", state, err)
	}
	if _, err := i.Store.DB.Exec(ctx, `UPDATE identity.invitation_delivery SET next_attempt=now()-interval '1 minute' WHERE invitation_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	m.fail = false
	if _, err := i.DeliverOne(ctx, m); err != nil {
		t.Fatal(err)
	}
	if m.calls != 2 || m.link != "https://app.test/invite#"+token {
		t.Fatal("delivery link")
	}
	if done, err := i.DeliverOne(ctx, m); err != nil || done {
		t.Fatal("duplicate delivery", err)
	}
	var p []byte
	if err := i.Store.DB.QueryRow(ctx, `SELECT payload FROM identity.invitation_delivery WHERE invitation_id=$1`, id).Scan(&p); err != nil || p != nil {
		t.Fatal("secret cleanup", err)
	}
}
func TestInvitationConfigurationAndEmail(t *testing.T) {
	for _, email := range []string{"User <u@example.test>", "u@example.test\r\nBcc: evil@example.test", "invalid"} {
		if _, err := invitationEmail(email); err == nil {
			t.Fatal(email)
		}
	}
	for _, origin := range []string{"https://evil.test/path", "http://public.test", "https://app.test?redirect=evil", "https://user:pass@app.test"} {
		if _, err := NewInvitations(Store{}, "issuer", origin, base64.StdEncoding.EncodeToString(make([]byte, 32))); err == nil {
			t.Fatal(origin)
		}
	}
}
