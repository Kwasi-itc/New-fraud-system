package platform

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type APIClient struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Scopes  []string  `json:"scopes"`
	Active  bool      `json:"active"`
	Version int64     `json:"version"`
}
type ClientChange struct {
	Name    string   `json:"name"`
	Scopes  []string `json:"scopes"`
	Active  bool     `json:"active"`
	Version int64    `json:"version"`
}
type ClientKey struct {
	ClientID     uuid.UUID `json:"client_id"`
	KeyID        string    `json:"kid"`
	PublicKeyPEM string    `json:"public_key_pem"`
	Active       bool      `json:"active"`
}
type ScopeApproval struct {
	Scope       string     `json:"scope"`
	RequestedBy uuid.UUID  `json:"requested_by"`
	ApprovedBy  *uuid.UUID `json:"approved_by"`
}

func (s Store) Clients(ctx context.Context, p auth.Principal, after uuid.UUID) ([]APIClient, error) {
	return s.FilteredClients(ctx, p, after, "")
}

func (s Store) Client(ctx context.Context, p auth.Principal, id uuid.UUID) (APIClient, error) {
	if !p.IsHuman() || !p.Can("clients:read") {
		return APIClient{}, ErrDenied
	}
	var out APIClient
	err := s.DB.QueryRow(ctx, `SELECT id,name,scopes,active,version FROM identity.clients WHERE tenant_id=$1 AND id=$2`, p.TenantID, id).Scan(&out.ID, &out.Name, &out.Scopes, &out.Active, &out.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return APIClient{}, &auth.Error{Code: 404, Message: "Client not found."}
	}
	return out, err
}

func (s Store) FilteredClients(ctx context.Context, p auth.Principal, after uuid.UUID, query string) ([]APIClient, error) {
	query = strings.TrimSpace(query)
	if !p.IsHuman() || !p.Can("clients:read") || len(query) > 100 {
		return nil, ErrDenied
	}
	statement, args := clientDirectoryQuery(p.TenantID, after, query)
	rows, err := s.DB.Query(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIClient{}
	for rows.Next() {
		var c APIClient
		if err = rows.Scan(&c.ID, &c.Name, &c.Scopes, &c.Active, &c.Version); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s Store) SaveClient(ctx context.Context, p auth.Principal, id uuid.UUID, in ClientChange) (APIClient, error) {
	in.Name = strings.TrimSpace(in.Name)
	if len(in.Name) < 1 || len(in.Name) > 100 {
		return APIClient{}, auth.Invalid("Client name must be 1–100 bytes.")
	}
	if len(in.Scopes) > 0 {
		if err := auth.ValidateCapabilities(in.Scopes, auth.PrincipalClient, false); err != nil {
			return APIClient{}, auth.Invalid(err.Error())
		}
	}
	creating := id == uuid.Nil
	if creating {
		id = uuid.New()
	} else if in.Version < 1 {
		return APIClient{}, auth.Invalid("Client version is required.")
	}
	if in.Scopes == nil {
		in.Scopes = []string{}
	}
	in.Scopes = slices.Clone(in.Scopes)
	slices.Sort(in.Scopes)
	out := APIClient{}
	err := s.administrativeTx(ctx, p, "clients:manage", func(tx pgx.Tx) error {
		var row pgx.Row
		if creating {
			row = tx.QueryRow(ctx, `INSERT INTO identity.clients(tenant_id,id,name,scopes,active) VALUES($1,$2,$3,$4,$5) RETURNING id,name,scopes,active,version`, p.TenantID, id, in.Name, in.Scopes, in.Active)
		} else {
			row = tx.QueryRow(ctx, `UPDATE identity.clients SET name=$3,scopes=$4,active=$5,version=version+1 WHERE tenant_id=$1 AND id=$2 AND version=$6 RETURNING id,name,scopes,active,version`, p.TenantID, id, in.Name, in.Scopes, in.Active, in.Version)
		}
		if err := row.Scan(&out.ID, &out.Name, &out.Scopes, &out.Active, &out.Version); errors.Is(err, pgx.ErrNoRows) {
			return ErrConflict
		} else if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM identity.client_scope_approvals WHERE tenant_id=$1 AND client_id=$2 AND NOT(scope=ANY($3::text[]))`, p.TenantID, id, in.Scopes); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO identity.client_scope_approvals(tenant_id,client_id,scope,requested_by)
   SELECT $1,$2,c.name,$4 FROM identity.capability_catalog c WHERE c.name=ANY($3::text[]) AND c.client_access='sensitive'
   ON CONFLICT DO NOTHING`, p.TenantID, id, in.Scopes, p.Subject); err != nil {
			return err
		}
		return audit(ctx, tx, p.TenantID, p.Subject, "client.saved", id, out)
	})
	return out, err
}
func (s Store) ClientKeys(ctx context.Context, p auth.Principal, client uuid.UUID, after string) ([]ClientKey, error) {
	rows, err := s.DB.Query(ctx, `SELECT client_id,kid,public_key_pem,active FROM identity.client_keys WHERE tenant_id=$1 AND client_id=$2 AND kid>$3 ORDER BY kid LIMIT 101`, p.TenantID, client, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ClientKey{}
	for rows.Next() {
		var k ClientKey
		if err = rows.Scan(&k.ClientID, &k.KeyID, &k.PublicKeyPEM, &k.Active); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}
func (s Store) RegisterClientKey(ctx context.Context, p auth.Principal, k ClientKey) error {
	if k.ClientID == uuid.Nil || len(k.KeyID) < 1 || len(k.KeyID) > 100 || k.KeyID != strings.TrimSpace(k.KeyID) || len(k.PublicKeyPEM) > 8192 {
		return auth.Invalid("Valid client ID, key ID and public key are required.")
	}
	fingerprint, err := auth.PublicKeyFingerprint([]byte(k.PublicKeyPEM))
	if err != nil {
		return auth.Invalid(err.Error())
	}
	return s.administrativeTx(ctx, p, "clients:manage", func(tx pgx.Tx) error {
		// Serialize rotations per client; key-count check and insert are atomic.
		var active bool
		if err := tx.QueryRow(ctx, `SELECT active FROM identity.clients WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, p.TenantID, k.ClientID).Scan(&active); errors.Is(err, pgx.ErrNoRows) {
			return ErrDenied
		} else if err != nil {
			return err
		}
		if !active {
			return ErrDenied
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM identity.client_keys WHERE tenant_id=$1 AND client_id=$2 AND active`, p.TenantID, k.ClientID).Scan(&count); err != nil {
			return err
		}
		if count >= 2 {
			return auth.Conflict()
		}
		if _, err := tx.Exec(ctx, `INSERT INTO identity.client_keys(tenant_id,client_id,kid,public_key_pem,fingerprint) VALUES($1,$2,$3,$4,$5)`, p.TenantID, k.ClientID, k.KeyID, k.PublicKeyPEM, fingerprint); err != nil {
			return err
		}
		return audit(ctx, tx, p.TenantID, p.Subject, "client.key_registered", k.ClientID, map[string]string{"kid": k.KeyID, "fingerprint": fingerprint})
	})
}
func (s Store) RevokeClientKey(ctx context.Context, p auth.Principal, client uuid.UUID, kid string) error {
	return s.administrativeTx(ctx, p, "clients:manage", func(tx pgx.Tx) error {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM identity.clients WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, p.TenantID, client).Scan(&id); errors.Is(err, pgx.ErrNoRows) {
			return ErrDenied
		} else if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE identity.client_keys SET active=false WHERE tenant_id=$1 AND client_id=$2 AND kid=$3 AND active`, p.TenantID, client, kid)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrConflict
		}
		return audit(ctx, tx, p.TenantID, p.Subject, "client.key_revoked", client, map[string]string{"kid": kid})
	})
}
func (s Store) ScopeApprovals(ctx context.Context, p auth.Principal, client uuid.UUID) ([]ScopeApproval, error) {
	rows, err := s.DB.Query(ctx, `SELECT scope,requested_by,approved_by FROM identity.client_scope_approvals WHERE tenant_id=$1 AND client_id=$2 ORDER BY scope LIMIT 101`, p.TenantID, client)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ScopeApproval{}
	for rows.Next() {
		var a ScopeApproval
		if err = rows.Scan(&a.Scope, &a.RequestedBy, &a.ApprovedBy); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s Store) ApproveScope(ctx context.Context, p auth.Principal, client uuid.UUID, scope string) error {
	def, ok := auth.LookupCapability(scope)
	if !ok || def.Client != "sensitive" {
		return auth.Invalid("Scope does not require sensitive approval.")
	}
	return s.administrativeTx(ctx, p, "clients:approve-sensitive", func(tx pgx.Tx) error {
		// Match issuance lock order: client before approval row.
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM identity.clients WHERE tenant_id=$1 AND id=$2 AND active FOR UPDATE`, p.TenantID, client).Scan(&id); errors.Is(err, pgx.ErrNoRows) {
			return ErrDenied
		} else if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE identity.client_scope_approvals SET approved_by=$4,approved_at=now() WHERE tenant_id=$1 AND client_id=$2 AND scope=$3 AND requested_by<>$4 AND approved_by IS NULL`, p.TenantID, client, scope, p.Subject)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrDenied
		}
		return audit(ctx, tx, p.TenantID, p.Subject, "client.scope_approved", client, map[string]string{"scope": scope})
	})
}

// ExchangeClientProof derives tenant and grants from the registered client.
// Assertion replay state intentionally survives only for this process lifetime.
func (s Server) ExchangeClientProof(ctx context.Context, client uuid.UUID, assertion, audience string, requested []string) (string, error) {
	if audience == "" {
		audience = auth.ClientAudience
	}
	if client == uuid.Nil || audience != auth.ClientAudience || s.TokenEndpointURL == "" || s.Replay == nil {
		return "", ErrDenied
	}
	untrusted, _, err := jwt.NewParser().ParseUnverified(assertion, &jwt.RegisteredClaims{})
	if err != nil {
		return "", ErrDenied
	}
	kid, _ := untrusted.Header["kid"].(string)
	if len(kid) < 1 || len(kid) > 100 {
		return "", ErrDenied
	}
	tx, err := s.Store.DB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var tenant uuid.UUID
	var scopes []string
	if err = tx.QueryRow(ctx, `SELECT c.tenant_id,c.scopes FROM identity.clients c JOIN core.tenants t ON t.id=c.tenant_id WHERE c.id=$1 AND c.active AND t.status='active' FOR SHARE OF c`, client).Scan(&tenant, &scopes); errors.Is(err, pgx.ErrNoRows) {
		return "", ErrDenied
	} else if err != nil {
		return "", err
	}
	var public string
	if err = tx.QueryRow(ctx, `SELECT public_key_pem FROM identity.client_keys WHERE tenant_id=$1 AND client_id=$2 AND kid=$3 AND active FOR SHARE`, tenant, client, kid).Scan(&public); errors.Is(err, pgx.ErrNoRows) {
		return "", ErrDenied
	} else if err != nil {
		return "", err
	}
	key, err := jwt.ParseRSAPublicKeyFromPEM([]byte(public))
	if err != nil {
		return "", err
	}
	now := time.Now()
	proof, err := auth.VerifyClientAssertion(assertion, client.String(), s.TokenEndpointURL, kid, key, now)
	if err != nil {
		return "", ErrDenied
	}
	rows, err := tx.Query(ctx, `SELECT scope FROM identity.client_scope_approvals WHERE tenant_id=$1 AND client_id=$2 AND approved_by IS NOT NULL ORDER BY scope FOR SHARE`, tenant, client)
	if err != nil {
		return "", err
	}
	approved := map[string]bool{}
	for rows.Next() {
		var scope string
		if err = rows.Scan(&scope); err != nil {
			rows.Close()
			return "", err
		}
		approved[scope] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", err
	}
	available := []string{}
	for _, scope := range scopes {
		def, ok := auth.LookupCapability(scope)
		if ok && (def.Client == "normal" || def.Client == "sensitive" && approved[scope]) {
			available = append(available, scope)
		}
	}
	granted := available
	if len(requested) > 0 {
		if err = auth.ValidateCapabilities(requested, auth.PrincipalClient, false); err != nil {
			return "", auth.Invalid("invalid_scope")
		}
		for _, scope := range requested {
			if !slices.Contains(available, scope) {
				return "", auth.Invalid("invalid_scope")
			}
		}
		granted = slices.Clone(requested)
	}
	if len(granted) == 0 {
		return "", auth.Invalid("invalid_scope")
	}
	if err = s.Replay.Consume(client.String(), proof.ID, proof.ExpiresAt.Add(auth.ClockLeeway), now); err != nil {
		return "", ErrDenied
	}
	claims := auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: s.Issuer, Subject: client.String(), Audience: jwt.ClaimStrings{audience}, ID: uuid.NewString(), IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(auth.ClientTTL))}, PrincipalType: auth.PrincipalClient, ClientID: client.String(), TenantID: tenant, Scopes: granted}
	token, err := auth.SignAccessToken(s.Key, s.KeyID, claims)
	if err != nil {
		return "", err
	}
	if err = audit(ctx, tx, tenant, client.String(), "client.token_issued", client, map[string]any{"audience": audience, "scopes": granted}); err != nil {
		return "", err
	}
	return token, tx.Commit(ctx)
}

func clientDirectoryQuery(tenant, after uuid.UUID, query string) (string, []any) {
	statement := `SELECT id,name,scopes,active,version FROM identity.clients WHERE tenant_id=$1 AND id>$2`
	args := []any{tenant, after}
	if query != "" {
		statement += ` AND lower(name) LIKE $3`
		args = append(args, strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(strings.ToLower(query))+"%")
	}
	return statement + ` ORDER BY id LIMIT 101`, args
}
