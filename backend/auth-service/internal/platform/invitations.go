package platform

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/mail"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/internal/oidcprovider"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// InvitationService owns invitation mutations; encrypted delivery material is
// separate from the digest used for acceptance and never returned by the API.
type InvitationService struct {
	Store          Store
	Issuer, Origin string
	Cipher         cipher.AEAD
}
type Invitation struct {
	ID       uuid.UUID `json:"id"`
	Email    string    `json:"email"`
	Roles    []string  `json:"roles"`
	Version  int64     `json:"version"`
	Status   string    `json:"status"`
	Expires  time.Time `json:"expires_at"`
	Delivery string    `json:"delivery"`
}
type deliveryPayload struct {
	Email string
	Token string
}

func NewInvitations(s Store, issuer, origin, key string) (*InvitationService, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"))) || issuer == "" {
		return nil, errors.New("invalid invitation origin or issuer")
	}
	b, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(b) != 32 {
		return nil, errors.New("invitation encryption key must be 32 base64-encoded bytes")
	}
	block, err := aes.NewCipher(b)
	if err != nil {
		return nil, err
	}
	a, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &InvitationService{s, issuer, strings.TrimSuffix(origin, "/"), a}, nil
}

// Preserve local-part case and reject display names/SMTP header injection. Only
// the domain is case-insensitive; never apply provider-specific dot/plus aliases.
func invitationEmail(v string) (string, error) {
	if len(v) > 254 || strings.ContainsAny(v, "\r\n") {
		return "", ErrDenied
	}
	v = strings.TrimSpace(v)
	a, err := mail.ParseAddress(v)
	if err != nil || a.Address != v || a.Name != "" {
		return "", ErrDenied
	}
	at := strings.LastIndexByte(v, '@')
	if at < 1 {
		return "", ErrDenied
	}
	return v[:at] + "@" + strings.ToLower(v[at+1:]), nil
}
func (i *InvitationService) seal(id uuid.UUID, email, token string) ([]byte, error) {
	p, err := json.Marshal(deliveryPayload{email, token})
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, i.Cipher.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return i.Cipher.Seal(nonce, nonce, p, []byte(id.String())), nil
}
func (i *InvitationService) open(id uuid.UUID, b []byte) (deliveryPayload, error) {
	var p deliveryPayload
	n := i.Cipher.NonceSize()
	if len(b) < n {
		return p, ErrDenied
	}
	raw, err := i.Cipher.Open(nil, b[:n], b[n:], []byte(id.String()))
	if err != nil {
		return p, err
	}
	err = json.Unmarshal(raw, &p)
	return p, err
}
func lockInvitationTenant(ctx context.Context, tx pgx.Tx, tenant uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "identity-admin:"+tenant.String())
	return err
}
func invitationAdmin(ctx context.Context, tx pgx.Tx, raw string) (Session, error) {
	c, err := session(ctx, tx, raw)
	if err != nil {
		return c, err
	}
	if err = lockInvitationTenant(ctx, tx, c.TenantID); err != nil {
		return c, err
	}
	var enabled bool
	if err = tx.QueryRow(ctx, `SELECT active FROM identity.users WHERE id=$1 FOR SHARE`, c.UserID).Scan(&enabled); err != nil {
		return c, err
	}
	if !enabled {
		return c, ErrDenied
	}
	if err = tx.QueryRow(ctx, `SELECT active AND 'tenant_admin'=ANY(roles) FROM identity.memberships WHERE tenant_id=$1 AND user_id=$2 FOR SHARE`, c.TenantID, c.UserID).Scan(&enabled); err != nil {
		return c, err
	}
	if !enabled {
		return c, ErrDenied
	}
	if err = tx.QueryRow(ctx, `SELECT revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_until>clock_timestamp() FROM identity.sessions WHERE id=$1 FOR SHARE`, c.ID).Scan(&enabled); err != nil {
		return c, err
	}
	if !enabled {
		return c, ErrDenied
	}
	c, err = session(ctx, tx, raw)
	if err != nil {
		return c, err
	}
	if !slices.Contains(c.Roles, "tenant_admin") {
		return c, ErrDenied
	}
	return c, nil
}
func (i *InvitationService) Create(ctx context.Context, raw string, id uuid.UUID, email string, roles []string) (uuid.UUID, error) {
	email, err := invitationEmail(email)
	if err != nil || id == uuid.Nil || !validRoles(roles) {
		return uuid.Nil, ErrDenied
	}
	tx, err := i.Store.DB.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(context.Background())
	c, err := invitationAdmin(ctx, tx, raw)
	if err != nil {
		return uuid.Nil, err
	}
	// Request UUID is an idempotency key. Replays never enqueue a second email.
	var priorEmail, priorIssuer string
	var priorRoles []string
	var priorTenant uuid.UUID
	err = tx.QueryRow(ctx, `SELECT tenant_id,email,issuer,roles FROM identity.invitations WHERE id=$1`, id).Scan(&priorTenant, &priorEmail, &priorIssuer, &priorRoles)
	if err == nil {
		a, b := slices.Clone(roles), slices.Clone(priorRoles)
		slices.Sort(a)
		slices.Sort(b)
		if priorTenant != c.TenantID || priorEmail != email || priorIssuer != i.Issuer || !slices.Equal(a, b) {
			return uuid.Nil, ErrConflict
		}
		return id, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, err
	}
	var duplicate bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM identity.invitations WHERE tenant_id=$1 AND email=$2 AND status='pending')`, c.TenantID, email).Scan(&duplicate); err != nil {
		return uuid.Nil, err
	}
	if duplicate {
		return uuid.Nil, ErrConflict
	}
	// Bound mail amplification per tenant, including multi-replica creation.
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM (SELECT 1 FROM identity.invitations WHERE tenant_id=$1 AND created_at>now()-interval '1 hour' LIMIT 100) q`, c.TenantID).Scan(&count); err != nil {
		return uuid.Nil, err
	}
	if count >= 100 {
		return uuid.Nil, ErrConflict
	}
	token := secret()
	payload, err := i.seal(id, email, token)
	if err != nil {
		return uuid.Nil, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO identity.invitations(id,tenant_id,email,issuer,roles,inviter_id,inviter_version,digest,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,now()+interval '48 hours')`, id, c.TenantID, email, i.Issuer, roles, c.UserID, c.Version, digest(token))
	if err != nil {
		return uuid.Nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO identity.invitation_delivery(invitation_id,generation,payload,state) VALUES($1,1,$2,'queued')`, id, payload); err != nil {
		return uuid.Nil, err
	}
	if err = audit(ctx, tx, c.TenantID, c.UserID.String(), "invitation.created", id, map[string]any{"roles": roles}); err != nil {
		return uuid.Nil, err
	}
	return id, tx.Commit(ctx)
}
func (i *InvitationService) Change(ctx context.Context, raw string, id uuid.UUID, version int64, action string) error {
	if action != "cancel" && action != "resend" {
		return ErrDenied
	}
	tx, err := i.Store.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	c, err := invitationAdmin(ctx, tx, raw)
	if err != nil {
		return err
	}
	var email, status string
	var v int64
	err = tx.QueryRow(ctx, `SELECT email,status,version FROM identity.invitations WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, id, c.TenantID).Scan(&email, &status, &v)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrDenied
	}
	if err != nil {
		return err
	}
	if status != "pending" || v != version {
		return ErrConflict
	}
	if action == "cancel" {
		if _, err = tx.Exec(ctx, `UPDATE identity.invitations SET status='cancelled',version=version+1 WHERE id=$1`, id); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE identity.invitation_delivery SET state='cancelled',payload=NULL,lease_id=NULL,lease_until=NULL WHERE invitation_id=$1`, id)
	} else {
		var recent bool
		if err = tx.QueryRow(ctx, `SELECT requested_at>now()-interval '1 minute' FROM identity.invitation_delivery WHERE invitation_id=$1`, id).Scan(&recent); err != nil {
			return err
		}
		if recent {
			return ErrConflict
		}
		token := secret()
		payload, e := i.seal(id, email, token)
		if e != nil {
			return e
		}
		if _, err = tx.Exec(ctx, `UPDATE identity.invitations SET digest=$2,version=version+1,expires_at=now()+interval '48 hours',inviter_id=$3,inviter_version=$4 WHERE id=$1`, id, digest(token), c.UserID, c.Version); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE identity.invitation_delivery SET generation=$2,payload=$3,state='queued',attempts=0,next_attempt=now(),requested_at=now(),lease_id=NULL,lease_until=NULL,sent_at=NULL WHERE invitation_id=$1`, id, v+1, payload)
	}
	if err != nil {
		return err
	}
	if err = audit(ctx, tx, c.TenantID, c.UserID.String(), "invitation."+action, id, struct{}{}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (i *InvitationService) List(ctx context.Context, raw string, after uuid.UUID) ([]Invitation, error) {
	c, err := i.Store.Resolve(ctx, raw)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(c.Roles, "tenant_admin") {
		return nil, ErrDenied
	}
	rows, err := i.Store.DB.Query(ctx, `SELECT i.id,i.email,i.roles,i.version,CASE WHEN i.status='pending' AND i.expires_at<=now() THEN 'expired' ELSE i.status END,i.expires_at,d.state FROM identity.invitations i JOIN identity.invitation_delivery d ON d.invitation_id=i.id WHERE i.tenant_id=$1 AND i.id>$2 ORDER BY i.id LIMIT 101`, c.TenantID, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invitation{}
	for rows.Next() {
		var v Invitation
		if err = rows.Scan(&v.ID, &v.Email, &v.Roles, &v.Version, &v.Status, &v.Expires, &v.Delivery); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (i *InvitationService) Begin(ctx context.Context, token string) (Login, error) {
	if !validSecret(token) {
		return Login{}, ErrDenied
	}
	l := Login{State: secret(), Binding: secret(), Nonce: secret(), Verifier: secret()}
	result, err := i.Store.DB.Exec(ctx, `INSERT INTO identity.login_transactions(digest,binding_digest,issuer,nonce,verifier,expires_at,invitation_digest) SELECT $1,$2,$3,$4,$5,now()+interval '5 minutes',$6 WHERE EXISTS(SELECT 1 FROM identity.invitations WHERE digest=$6 AND issuer=$3 AND status='pending' AND expires_at>now())`, digest(l.State), digest(l.Binding), i.Issuer, l.Nonce, l.Verifier, digest(token))
	if err != nil {
		return Login{}, err
	}
	if result.RowsAffected() != 1 {
		return Login{}, ErrDenied
	}
	return l, nil
}
func (i *InvitationService) Accept(ctx context.Context, hash []byte, id oidcprovider.Identity) (string, error) {
	email, err := invitationEmail(id.Email)
	if err != nil || !id.EmailVerified || id.Issuer != i.Issuer || strings.TrimSpace(id.Subject) == "" || len(id.Subject) > 1024 || len(hash) != 32 {
		return "", ErrDenied
	}
	tx, err := i.Store.DB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(context.Background())
	var invite, tenant, inviter uuid.UUID
	var version int64
	err = tx.QueryRow(ctx, `SELECT id,tenant_id,inviter_id,inviter_version FROM identity.invitations WHERE digest=$1 AND issuer=$2 AND email=$3 AND status='pending' AND expires_at>clock_timestamp()`, hash, id.Issuer, email).Scan(&invite, &tenant, &inviter, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrDenied
	}
	if err != nil {
		return "", err
	}
	if err = lockInvitationTenant(ctx, tx, tenant); err != nil {
		return "", err
	}
	// Same identity serialization key as operator provisioning; no email auto-link.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, id.Issuer+"\n"+id.Subject); err != nil {
		return "", err
	}
	var active bool
	if err = tx.QueryRow(ctx, `SELECT active FROM identity.users WHERE id=$1 FOR SHARE`, inviter).Scan(&active); err != nil {
		return "", err
	}
	if !active {
		return "", ErrDenied
	}
	if err = tx.QueryRow(ctx, `SELECT active AND version=$3 AND 'tenant_admin'=ANY(roles) FROM identity.memberships WHERE tenant_id=$1 AND user_id=$2 FOR SHARE`, tenant, inviter, version).Scan(&active); err != nil {
		return "", err
	}
	if !active {
		return "", ErrDenied
	}
	if err = tx.QueryRow(ctx, `SELECT status='active' FROM core.tenants WHERE id=$1 FOR SHARE`, tenant).Scan(&active); err != nil {
		return "", err
	}
	if !active {
		return "", ErrDenied
	}
	var roles []string
	err = tx.QueryRow(ctx, `SELECT roles FROM identity.invitations WHERE id=$1 AND digest=$2 AND status='pending' AND expires_at>clock_timestamp() FOR UPDATE`, invite, hash).Scan(&roles)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrDenied
	}
	if err != nil {
		return "", err
	}
	if !validRoles(roles) {
		return "", ErrDenied
	}
	var user uuid.UUID
	err = tx.QueryRow(ctx, `SELECT user_id FROM identity.external_identities WHERE issuer=$1 AND subject=$2`, id.Issuer, id.Subject).Scan(&user)
	if errors.Is(err, pgx.ErrNoRows) {
		user = uuid.New()
		name := strings.TrimSpace(id.Name)
		if name == "" || len(name) > 200 {
			name = "Invited user"
		}
		if _, err = tx.Exec(ctx, `INSERT INTO identity.users(id,display_name) VALUES($1,$2)`, user, name); err != nil {
			return "", err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO identity.external_identities(issuer,subject,user_id) VALUES($1,$2,$3)`, id.Issuer, id.Subject, user); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	if err = tx.QueryRow(ctx, `SELECT active FROM identity.users WHERE id=$1 FOR SHARE`, user).Scan(&active); err != nil {
		return "", err
	}
	if !active {
		return "", ErrDenied
	}
	result, err := tx.Exec(ctx, `INSERT INTO identity.memberships(tenant_id,user_id,roles) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, tenant, user, roles)
	if err != nil {
		return "", err
	}
	if result.RowsAffected() != 1 {
		return "", ErrConflict
	}
	if _, err = tx.Exec(ctx, `UPDATE identity.invitations SET status='accepted',accepted_by=$2,accepted_at=now(),version=version+1 WHERE id=$1`, invite, user); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `UPDATE identity.invitation_delivery SET payload=NULL,state=CASE WHEN state='sent' THEN state ELSE 'cancelled' END,lease_id=NULL,lease_until=NULL WHERE invitation_id=$1`, invite); err != nil {
		return "", err
	}
	raw := secret()
	sid := uuid.New()
	if _, err = tx.Exec(ctx, `INSERT INTO identity.sessions(id,digest,user_id,tenant_id,expires_at,idle_until) VALUES($1,$2,$3,$4,now()+interval '8 hours',now()+interval '30 minutes')`, sid, digest(raw), user, tenant); err != nil {
		return "", err
	}
	if err = audit(ctx, tx, tenant, user.String(), "invitation.accepted", invite, map[string]any{"roles": roles, "inviter_id": inviter}); err != nil {
		return "", err
	}
	if err = audit(ctx, tx, tenant, user.String(), "session.created", sid, struct{}{}); err != nil {
		return "", err
	}
	return raw, tx.Commit(ctx)
}
