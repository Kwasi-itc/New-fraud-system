package mysqlauth

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/internal/oidcprovider"
	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/internal/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/sessioncredential"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type Invitations struct {
	Store    *Store
	Envelope *platform.InvitationEnvelope
}

var _ platform.InvitationBackend = (*Invitations)(nil)

func NewInvitations(store *Store, issuer, origin, key string) (*Invitations, error) {
	e, err := platform.NewInvitationEnvelope(issuer, origin, key)
	if err != nil {
		return nil, err
	}
	if store == nil {
		return nil, errors.New("invitation store required")
	}
	return &Invitations{store, e}, nil
}

func (i *Invitations) Create(ctx context.Context, raw string, invite uuid.UUID, email string, roles []string) (uuid.UUID, error) {
	var e error
	email, e = platform.NormalizeInvitationEmail(email)
	if e != nil || invite == uuid.Nil || !rolesValid(roles) {
		return uuid.Nil, denied
	}
	s := i.Store
	e = s.transaction(ctx, func(tx *sql.Tx) error {
		c, e := s.caller(ctx, tx, raw, true, true)
		if e != nil {
			return e
		}
		var tenant mysqlstore.UUID
		var priorEmail, issuer, priorRoles string
		e = tx.QueryRowContext(ctx, "SELECT tenant_id,email,issuer,roles FROM "+s.table("identity_invitations")+" WHERE id=?", id(invite)).Scan(&tenant, &priorEmail, &issuer, &priorRoles)
		if e == nil {
			a, b := slices.Clone(roles), rolesList(priorRoles)
			slices.Sort(a)
			slices.Sort(b)
			if uuid.UUID(tenant) != c.TenantID || priorEmail != email || issuer != i.Envelope.Issuer || !slices.Equal(a, b) {
				return conflict
			}
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		var n int
		if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM (SELECT id FROM "+s.table("identity_invitations")+" WHERE tenant_id=? AND created_at>TIMESTAMPADD(HOUR,-1,SYSDATE(6)) LIMIT 100) recent", id(c.TenantID)).Scan(&n); e != nil {
			return e
		}
		if n >= 100 {
			return conflict
		}
		var exists bool
		if e = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+s.table("identity_invitations")+" WHERE tenant_id=? AND pending_email=?)", id(c.TenantID), email).Scan(&exists); e != nil {
			return e
		}
		if exists {
			return conflict
		}
		token, e := secret()
		if e != nil {
			return e
		}
		payload, e := i.Envelope.Seal(invite, email, token)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO "+s.table("identity_invitations")+"(id,tenant_id,email,issuer,roles,inviter_id,inviter_version,digest,expires_at) VALUES(?,?,?,?,?,?,?,?,TIMESTAMPADD(HOUR,48,SYSDATE(6)))", id(invite), id(c.TenantID), email, i.Envelope.Issuer, strings.Join(roles, ","), id(c.UserID), c.Version, sessioncredential.Digest(token)); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO "+s.table("identity_invitation_delivery")+"(invitation_id,generation,payload,state) VALUES(?,1,?,'queued')", id(invite), payload); e != nil {
			return e
		}
		return s.audit(ctx, tx, c.TenantID, c.UserID.String(), "invitation.created", invite, map[string]any{"roles": roles})
	})
	return invite, e
}
func (i *Invitations) Change(ctx context.Context, raw string, invite uuid.UUID, version int64, action string) error {
	if invite == uuid.Nil || version < 1 || (action != "cancel" && action != "resend") {
		return denied
	}
	s := i.Store
	return s.transaction(ctx, func(tx *sql.Tx) error {
		c, e := s.caller(ctx, tx, raw, true, true)
		if e != nil {
			return e
		}
		var email, status string
		var v int64
		if e = tx.QueryRowContext(ctx, "SELECT email,status,version FROM "+s.table("identity_invitations")+" WHERE id=? AND tenant_id=? FOR UPDATE", id(invite), id(c.TenantID)).Scan(&email, &status, &v); e != nil {
			return noRow(e)
		}
		if v != version || status != "pending" {
			return conflict
		}
		// Claiming only locks delivery rows; mutations consistently take invitation
		// before delivery and recheck authority after their final lock wait.
		var recent bool
		if e = tx.QueryRowContext(ctx, "SELECT requested_at>TIMESTAMPADD(MINUTE,-1,SYSDATE(6)) FROM "+s.table("identity_invitation_delivery")+" WHERE invitation_id=? FOR UPDATE", id(invite)).Scan(&recent); e != nil {
			return e
		}
		if _, e = s.session(ctx, tx, raw); e != nil {
			return e
		}
		if action == "cancel" {
			if _, e = tx.ExecContext(ctx, "UPDATE "+s.table("identity_invitations")+" SET status='cancelled',version=version+1 WHERE id=?", id(invite)); e != nil {
				return e
			}
			_, e = tx.ExecContext(ctx, "UPDATE "+s.table("identity_invitation_delivery")+" SET state='cancelled',payload=NULL,lease_id=NULL,lease_until=NULL WHERE invitation_id=?", id(invite))
		} else {
			if recent {
				return conflict
			}
			token, err := secret()
			if err != nil {
				return err
			}
			payload, err := i.Envelope.Seal(invite, email, token)
			if err != nil {
				return err
			}
			if _, e = tx.ExecContext(ctx, "UPDATE "+s.table("identity_invitations")+" SET digest=?,version=version+1,expires_at=TIMESTAMPADD(HOUR,48,SYSDATE(6)),inviter_id=?,inviter_version=? WHERE id=?", sessioncredential.Digest(token), id(c.UserID), c.Version, id(invite)); e != nil {
				return e
			}
			_, e = tx.ExecContext(ctx, "UPDATE "+s.table("identity_invitation_delivery")+" SET generation=?,payload=?,state='queued',attempts=0,next_attempt=SYSDATE(6),requested_at=SYSDATE(6),lease_id=NULL,lease_until=NULL,sent_at=NULL WHERE invitation_id=?", v+1, payload, id(invite))
		}
		if e != nil {
			return e
		}
		return s.audit(ctx, tx, c.TenantID, c.UserID.String(), "invitation."+action, invite, struct{}{})
	})
}
func (i *Invitations) List(ctx context.Context, raw string, after uuid.UUID) ([]platform.Invitation, error) {
	s := i.Store
	out := []platform.Invitation{}
	e := s.transaction(ctx, func(tx *sql.Tx) error {
		c, e := s.caller(ctx, tx, raw, false, true)
		if e != nil {
			return e
		}
		rows, e := tx.QueryContext(ctx, "SELECT i.id,i.email,i.roles,i.version,CASE WHEN i.status='pending' AND i.expires_at<=SYSDATE(6) THEN 'expired' ELSE i.status END,i.expires_at,d.state FROM "+s.table("identity_invitations")+" i JOIN "+s.table("identity_invitation_delivery")+" d ON d.invitation_id=i.id WHERE i.tenant_id=? AND i.id>? ORDER BY i.id LIMIT 101", id(c.TenantID), id(after))
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var v platform.Invitation
			var u mysqlstore.UUID
			var roles string
			if e = rows.Scan(&u, &v.Email, &roles, &v.Version, &v.Status, &v.Expires, &v.Delivery); e != nil {
				return e
			}
			v.ID = uuid.UUID(u)
			v.Roles = rolesList(roles)
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, e
}
func (i *Invitations) Begin(ctx context.Context, token string) (platform.Login, error) {
	if !sessioncredential.Valid(token) {
		return platform.Login{}, denied
	}
	s := i.Store
	l, e := newLogin()
	if e != nil {
		return l, e
	}
	r, e := s.DB.ExecContext(ctx, "INSERT INTO "+s.table("identity_login_transactions")+"(digest,binding_digest,issuer,nonce,verifier,expires_at,invitation_digest) SELECT ?,?,?,?,?,TIMESTAMPADD(MINUTE,5,SYSDATE(6)),? WHERE EXISTS(SELECT 1 FROM "+s.table("identity_invitations")+" WHERE digest=? AND issuer=? AND status='pending' AND expires_at>SYSDATE(6))", sessioncredential.Digest(l.State), sessioncredential.Digest(l.Binding), i.Envelope.Issuer, l.Nonce, l.Verifier, sessioncredential.Digest(token), sessioncredential.Digest(token), i.Envelope.Issuer)
	if e != nil {
		return l, e
	}
	n, e := r.RowsAffected()
	if e == nil && n != 1 {
		e = denied
	}
	return l, e
}
func (i *Invitations) Accept(ctx context.Context, hash []byte, identity oidcprovider.Identity) (string, error) {
	email, e := platform.NormalizeInvitationEmail(identity.Email)
	if e != nil || !identity.EmailVerified || identity.Issuer != i.Envelope.Issuer || len(hash) != 32 {
		return "", denied
	}
	if _, e = identityKey(identity.Issuer, identity.Subject); e != nil {
		return "", e
	}
	s := i.Store
	var raw string
	e = s.transaction(ctx, func(tx *sql.Tx) error {
		var invite, tenant, inviter mysqlstore.UUID
		var inviterVersion int64
		e := tx.QueryRowContext(ctx, "SELECT id,tenant_id,inviter_id,inviter_version FROM "+s.table("identity_invitations")+" WHERE digest=? AND issuer=? AND email=? AND status='pending' AND expires_at>SYSDATE(6)", hash, identity.Issuer, email).Scan(&invite, &tenant, &inviter, &inviterVersion)
		if e != nil {
			return noRow(e)
		}
		if e = s.lockAdmin(ctx, tx, uuid.UUID(tenant)); e != nil {
			return e
		}
		// Identity-key guard precedes user locks in both acceptance and provisioning.
		name := strings.TrimSpace(identity.Name)
		if name == "" || len(name) > 200 {
			name = "Invited user"
		}
		user, e := s.external(ctx, tx, identity.Issuer, identity.Subject, name)
		if e != nil {
			return e
		}
		users := []uuid.UUID{user, uuid.UUID(inviter)}
		slices.SortFunc(users, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
		for _, u := range users {
			var active bool
			if e = tx.QueryRowContext(ctx, "SELECT active FROM "+s.table("identity_users")+" WHERE id=? FOR SHARE", id(u)).Scan(&active); e != nil {
				return noRow(e)
			}
			if !active {
				return denied
			}
		}
		var active bool
		if e = tx.QueryRowContext(ctx, "SELECT active AND version=? AND FIND_IN_SET('tenant_admin',roles)>0 FROM "+s.table("identity_memberships")+" WHERE tenant_id=? AND user_id=? FOR SHARE", inviterVersion, tenant, inviter).Scan(&active); e != nil {
			return noRow(e)
		}
		if !active {
			return denied
		}
		if e = tx.QueryRowContext(ctx, "SELECT active FROM "+s.table("identity_tenants")+" WHERE id=? FOR SHARE", tenant).Scan(&active); e != nil {
			return noRow(e)
		}
		if !active {
			return denied
		}
		var roles string
		var valid bool
		var currentInviter mysqlstore.UUID
		var currentVersion int64
		if e = tx.QueryRowContext(ctx, "SELECT roles,expires_at>SYSDATE(6) AND status='pending' AND digest=?,inviter_id,inviter_version FROM "+s.table("identity_invitations")+" WHERE id=? FOR UPDATE", hash, invite).Scan(&roles, &valid, &currentInviter, &currentVersion); e != nil {
			return noRow(e)
		}
		if !valid || currentInviter != inviter || currentVersion != inviterVersion {
			return denied
		}
		// Evaluate expiry again after the invitation row lock has waited.
		if e = tx.QueryRowContext(ctx, "SELECT expires_at>SYSDATE(6) FROM "+s.table("identity_invitations")+" WHERE id=?", invite).Scan(&valid); e != nil {
			return e
		}
		if !valid {
			return denied
		}
		var exists bool
		if e = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+s.table("identity_memberships")+" WHERE tenant_id=? AND user_id=?)", tenant, id(user)).Scan(&exists); e != nil {
			return e
		}
		if exists {
			return conflict
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO "+s.table("identity_memberships")+"(tenant_id,user_id,roles,active,version) VALUES(?,?,?,TRUE,1)", tenant, id(user), roles); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "UPDATE "+s.table("identity_invitations")+" SET status='accepted',accepted_by=?,accepted_at=SYSDATE(6),version=version+1 WHERE id=?", id(user), invite); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "UPDATE "+s.table("identity_invitation_delivery")+" SET payload=NULL,state=CASE WHEN state='sent' THEN state ELSE 'cancelled' END,lease_id=NULL,lease_until=NULL WHERE invitation_id=?", invite); e != nil {
			return e
		}
		var sid uuid.UUID
		raw, sid, e = s.insertSession(ctx, tx, user, uuid.UUID(tenant))
		if e != nil {
			return e
		}
		if e = s.audit(ctx, tx, uuid.UUID(tenant), user.String(), "invitation.accepted", uuid.UUID(invite), map[string]any{"roles": rolesList(roles), "inviter_id": uuid.UUID(inviter)}); e != nil {
			return e
		}
		return s.audit(ctx, tx, uuid.UUID(tenant), user.String(), "session.created", sid, struct{}{})
	})
	return raw, e
}
