package platform

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func testBFF(t *testing.T) (*auth.Verifier, string) {
	t.Helper()
	key, pub := clientTestKey(t)
	cfg := auth.TrustedKey{ID: "bff", PublicKey: pub, Issuer: "urn:fraud:service:frontend-bff", ServiceID: "frontend-bff", PrincipalTypes: []auth.PrincipalType{auth.PrincipalService}, Capabilities: []string{"auth:bootstrap"}, Audiences: []string{"urn:fraud:auth-service"}}
	v, err := auth.NewTrustedVerifier(auth.KeyRing{Keys: []auth.TrustedKey{cfg}}, "urn:fraud:auth-service")
	if err != nil {
		t.Fatal(err)
	}
	signer := auth.Signer{Key: key, KeyID: "bff", ServiceID: "frontend-bff"}
	token, err := signer.Sign(uuid.Nil, "urn:fraud:auth-service", []string{"auth:bootstrap"}, auth.Principal{})
	if err != nil {
		t.Fatal(err)
	}
	return v, token
}

func testBFFAndPlatform(t *testing.T, platformKeyID, platformPublicKey string) (*auth.Verifier, string) {
	t.Helper()
	key, publicKey := clientTestKey(t)
	trust := auth.KeyRing{Keys: []auth.TrustedKey{
		{ID: "bff", PublicKey: publicKey, Issuer: "urn:fraud:service:frontend-bff", ServiceID: "frontend-bff", PrincipalTypes: []auth.PrincipalType{auth.PrincipalService}, Capabilities: []string{"auth:bootstrap"}, Audiences: []string{"urn:fraud:auth-service"}},
		{ID: platformKeyID, PublicKey: platformPublicKey, Issuer: auth.PlatformIssuer, PrincipalTypes: []auth.PrincipalType{auth.PrincipalUser}, Audiences: []string{auth.PlatformAudience}},
	}}
	verifier, err := auth.NewTrustedVerifier(trust, "urn:fraud:auth-service", auth.PlatformAudience)
	if err != nil {
		t.Fatal(err)
	}
	signer := auth.Signer{Key: key, KeyID: "bff", ServiceID: "frontend-bff"}
	token, err := signer.Sign(uuid.Nil, "urn:fraud:auth-service", []string{"auth:bootstrap"}, auth.Principal{})
	if err != nil {
		t.Fatal(err)
	}
	return verifier, token
}

func testHumanToken(t *testing.T, keyID string, key *rsa.PrivateKey, current Session) string {
	t.Helper()
	now := time.Now()
	claims := auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: auth.PlatformIssuer, Audience: jwt.ClaimStrings{auth.PlatformAudience},
			Subject: current.UserID.String(), ID: uuid.NewString(),
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(auth.UserTTL)),
		},
		PrincipalType: auth.PrincipalUser, TenantID: current.TenantID,
		Roles: current.Roles, Capabilities: current.Capabilities,
		SessionID: current.ID, AuthzVersion: current.Version,
	}
	token, err := auth.SignAccessToken(key, keyID, claims)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func clientTestKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	data, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return key, string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: data}))
}
func TestCustomRoleGrantsRevocationAndTenantIsolation(t *testing.T) {
	s, tenant := fixture(t)
	ctx := context.Background()
	_, adminRaw := provision(t, s, tenant, "admin", "tenant_admin")
	member, memberRaw := provision(t, s, tenant, "member", "platform_viewer")
	session, err := s.Resolve(ctx, adminRaw)
	if err != nil {
		t.Fatal(err)
	}
	admin := principal(session)
	for _, cap := range auth.Catalog() {
		if cap.Human && !admin.Can(cap.Name) {
			t.Fatalf("tenant admin missing %s", cap.Name)
		}
	}
	if _, err = s.SaveRole(ctx, admin, "", RoleChange{Name: "Backdoor", Capabilities: []string{"roles:manage"}}); err == nil {
		t.Fatal("reserved capability entered custom role")
	}
	custom, err := s.SaveRole(ctx, admin, "", RoleChange{Name: "Risk Analyst", Capabilities: []string{"cases:read", "cases:review"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ChangeMember(ctx, adminRaw, member, 1, []string{custom.ID}, true); err != nil {
		t.Fatal(err)
	}
	memberRaw, err = s.CreateSession(ctx, "https://provider.test", "member")
	if err != nil {
		t.Fatal(err)
	}
	session, err = s.Resolve(ctx, memberRaw)
	if err != nil {
		t.Fatal(err)
	}
	p := principal(session)
	if !p.Can("cases:review") || p.Can("roles:manage") {
		t.Fatal("resolved custom bundle", session)
	}
	if err = auth.CheckCapability(ctx, s.DB, p, "cases:review", false); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveRole(ctx, p, custom.ID, RoleChange{Name: "Risk Analyst", Capabilities: []string{"cases:read"}, Version: 1}); err == nil {
		t.Fatal("custom user edited role")
	}
	custom, err = s.SaveRole(ctx, admin, custom.ID, RoleChange{Name: "Risk Analyst", Capabilities: []string{"cases:read"}, Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err = auth.CheckCapability(ctx, s.DB, p, "cases:review", false); err == nil {
		t.Fatal("role edit failed to revoke hardened authority")
	}
	if err = auth.CheckCapability(ctx, s.DB, p, "cases:read", false); err != nil {
		t.Fatal("remaining capability lost", err)
	}
	if err = s.DeleteRole(ctx, admin, custom.ID, custom.Version); err == nil {
		t.Fatal("in-use role deleted")
	}
	foreign := admin
	foreign.TenantID = uuid.New()
	if _, err = s.SaveRole(ctx, foreign, "", RoleChange{Name: "Foreign", Capabilities: []string{"cases:read"}}); err == nil {
		t.Fatal("foreign tenant role created")
	}
	if _, err = s.SaveRole(ctx, admin, "tenant_admin", RoleChange{Name: "Changed", Capabilities: []string{"cases:read"}, Version: 1}); err == nil {
		t.Fatal("builtin role changed")
	}
}
func TestRoleAuditFailureRollsBack(t *testing.T) {
	s, tenant := fixture(t)
	ctx := context.Background()
	_, raw := provision(t, s, tenant, "admin", "tenant_admin")
	session, err := s.Resolve(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(ctx, `CREATE FUNCTION identity.fail_role_audit() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.action='role.saved' THEN RAISE EXCEPTION 'audit outage'; END IF; RETURN NEW; END$$; CREATE TRIGGER fail_role_audit BEFORE INSERT ON identity.audit_events FOR EACH ROW EXECUTE FUNCTION identity.fail_role_audit()`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveRole(ctx, principal(session), "", RoleChange{Name: "Rollback", Capabilities: []string{"cases:read"}}); err == nil {
		t.Fatal("audit failure ignored")
	}
	var count int
	if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM identity.roles WHERE tenant_id=$1 AND name='Rollback'`, tenant).Scan(&count); err != nil || count != 0 {
		t.Fatal("role survived audit failure", count, err)
	}
}
func clientAssertion(t *testing.T, key *rsa.PrivateKey, kid string, client uuid.UUID, endpoint, jti string) string {
	t.Helper()
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{Issuer: client.String(), Subject: client.String(), Audience: jwt.ClaimStrings{endpoint}, ID: jti, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute))})
	token.Header["kid"] = kid
	raw, err := token.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestClientProofExchangeApprovalRotationAndReplay(t *testing.T) {
	s, tenant := fixture(t)
	ctx := context.Background()
	_, adminRaw := provision(t, s, tenant, "admin", "tenant_admin")
	_, approverRaw := provision(t, s, tenant, "approver", "tenant_admin")
	a, err := s.Resolve(ctx, adminRaw)
	if err != nil {
		t.Fatal(err)
	}
	admin := principal(a)
	b, err := s.Resolve(ctx, approverRaw)
	if err != nil {
		t.Fatal(err)
	}
	approver := principal(b)
	client, err := s.SaveClient(ctx, admin, uuid.Nil, ClientChange{Name: "Bank", Scopes: []string{"ingest:write", "decisions:evaluate", "decisions:read", "cases:read"}, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ApproveScope(ctx, admin, client.ID, "cases:read"); err == nil {
		t.Fatal("requester approved own sensitive scope")
	}
	clientKey, pub := clientTestKey(t)
	if err = s.RegisterClientKey(ctx, admin, ClientKey{ClientID: client.ID, KeyID: "first", PublicKeyPEM: pub}); err != nil {
		t.Fatal(err)
	}
	secondKey, secondPub := clientTestKey(t)
	_ = secondKey
	thirdKey, thirdPub := clientTestKey(t)
	_ = thirdKey
	var successes atomic.Int32
	var wg sync.WaitGroup
	for kid, pem := range map[string]string{"second": secondPub, "third": thirdPub} {
		wg.Add(1)
		go func(k, p string) {
			defer wg.Done()
			if s.RegisterClientKey(ctx, admin, ClientKey{ClientID: client.ID, KeyID: k, PublicKeyPEM: p}) == nil {
				successes.Add(1)
			}
		}(kid, pem)
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatal("rotation exceeded two active keys", successes.Load())
	}
	platformKey, platformPub := clientTestKey(t)
	app := Server{Store: s, Key: platformKey, KeyID: "platform", Issuer: auth.PlatformIssuer, TokenEndpointURL: "https://fraud.test/oauth/token", Replay: auth.NewReplayCache(100)}
	verifier, err := auth.NewTrustedVerifier(auth.KeyRing{Keys: []auth.TrustedKey{{ID: "platform", PublicKey: platformPub, Issuer: auth.PlatformIssuer, PrincipalTypes: []auth.PrincipalType{auth.PrincipalClient}, Audiences: []string{auth.ClientAudience}}}}, auth.ClientAudience)
	if err != nil {
		t.Fatal(err)
	}
	defaultProof := clientAssertion(t, clientKey, "first", client.ID, app.TokenEndpointURL, "default-system-audience")
	defaultToken, err := app.ExchangeClientProof(ctx, client.ID, defaultProof, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defaultPrincipal, err := verifier.Verify(defaultToken)
	if err != nil || !defaultPrincipal.Can("ingest:write") || !defaultPrincipal.Can("decisions:evaluate") || !defaultPrincipal.Can("decisions:read") || defaultPrincipal.Can("cases:read") {
		t.Fatal("default token did not preserve normal grants and sensitive approval boundary", err)
	}
	for _, badAudience := range []string{"urn:fraud:ingestion-service", "urn:fraud:decision-engine-service", "urn:fraud:case-manager-service", "urn:other:system"} {
		proof := clientAssertion(t, clientKey, "first", client.ID, app.TokenEndpointURL, uuid.NewString())
		if _, err := app.ExchangeClientProof(ctx, client.ID, proof, badAudience, []string{"ingest:write"}); !errors.Is(err, ErrDenied) {
			t.Fatal("API-specific or unknown audience accepted", badAudience, err)
		}
	}
	for _, scope := range []string{"cases:read", "records:read", "roles:manage", "unknown:scope"} {
		proof := clientAssertion(t, clientKey, "first", client.ID, app.TokenEndpointURL, uuid.NewString())
		if _, err := app.ExchangeClientProof(ctx, client.ID, proof, auth.ClientAudience, []string{scope}); err == nil {
			t.Fatal("unapproved, ungranted or ineligible scope accepted", scope)
		}
	}
	if err = s.ApproveScope(ctx, approver, client.ID, "cases:read"); err != nil {
		t.Fatal(err)
	}
	proof := clientAssertion(t, clientKey, "first", client.ID, app.TokenEndpointURL, "one-proof")
	var exchanged atomic.Int32
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := app.ExchangeClientProof(ctx, client.ID, proof, auth.ClientAudience, []string{"ingest:write", "decisions:evaluate", "decisions:read"}); e == nil {
				exchanged.Add(1)
			}
		}()
	}
	wg.Wait()
	if exchanged.Load() != 1 {
		t.Fatal("assertion replay succeeded", exchanged.Load())
	}
	proof = clientAssertion(t, clientKey, "first", client.ID, app.TokenEndpointURL, "case-proof")
	raw, err := app.ExchangeClientProof(ctx, client.ID, proof, auth.ClientAudience, []string{"cases:read"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := verifier.Verify(raw)
	if err != nil || p.TenantID != tenant || !p.IsClient() || !p.Can("cases:read") {
		t.Fatal("issued wrong client principal", p, err)
	}
	if slices.Contains(p.Roles, "tenant_admin") {
		t.Fatal("client inherited role")
	}
	if err = s.RevokeClientKey(ctx, admin, client.ID, "first"); err != nil {
		t.Fatal(err)
	}
	proof = clientAssertion(t, clientKey, "first", client.ID, app.TokenEndpointURL, "after-revoke")
	if _, err = app.ExchangeClientProof(ctx, client.ID, proof, auth.ClientAudience, nil); !errors.Is(err, ErrDenied) {
		t.Fatal("revoked key still issued", err)
	}
	if _, err = verifier.Verify(raw); err != nil {
		t.Fatal("stateless issued token unexpectedly revoked", err)
	}
}

func TestSQLCatalogAndPredefinedBundlesMatchCanonicalContract(t *testing.T) {
	s, tenant := fixture(t)
	ctx := context.Background()
	rows, err := s.DB.Query(ctx, `SELECT name,human,service,client_access,reserved FROM identity.capability_catalog ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	got := []auth.Capability{}
	for rows.Next() {
		var c auth.Capability
		if err := rows.Scan(&c.Name, &c.Human, &c.Service, &c.Client, &c.Reserved); err != nil {
			t.Fatal(err)
		}
		got = append(got, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, auth.Catalog()) {
		t.Fatal("SQL capability catalog drifted from the shared contract; regenerate schema_v5_seed.sql")
	}
	for role, expected := range auth.PredefinedRoles() {
		var actual []string
		if err := s.DB.QueryRow(ctx, `SELECT capabilities FROM identity.roles WHERE tenant_id=$1 AND id=$2 AND builtin`, tenant, role).Scan(&actual); err != nil {
			t.Fatal(err)
		}
		slices.Sort(actual)
		slices.Sort(expected)
		if !slices.Equal(actual, expected) {
			t.Fatal("SQL role bundle drift", role)
		}
	}
}

func TestConcurrentCustomRoleEditsUseOptimisticVersions(t *testing.T) {
	s, tenant := fixture(t)
	ctx := context.Background()
	_, raw := provision(t, s, tenant, "role-admin", "tenant_admin")
	session, err := s.Resolve(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	p := principal(session)
	role, err := s.SaveRole(ctx, p, "", RoleChange{Name: "Concurrent role", Capabilities: []string{"cases:read"}})
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	var group sync.WaitGroup
	for _, name := range []string{"First edit", "Second edit"} {
		group.Add(1)
		go func(name string) {
			defer group.Done()
			_, err := s.SaveRole(ctx, p, role.ID, RoleChange{Name: name, Capabilities: []string{"cases:read"}, Version: role.Version})
			results <- err
		}(name)
	}
	group.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatal("role edits lost an optimistic conflict", success, conflicts)
	}
}

func TestCapabilityResolutionUsesBoundedActorGrantLookup(t *testing.T) {
	s, tenant := fixture(t)
	ctx := context.Background()
	user, _ := provision(t, s, tenant, "plan-user", "case_investigator")
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := s.DB.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	roles := make([]string, 32)
	for i := range roles {
		roles[i] = "role_" + uuid.NewString()
		exec(`INSERT INTO identity.roles(tenant_id,id,name,capabilities) VALUES($1,$2,$2,ARRAY['models:read','records:read'])`, tenant, roles[i])
	}
	exec(`UPDATE identity.memberships SET roles=$3 WHERE tenant_id=$1 AND user_id=$2`, tenant, user, roles[:1])
	exec(`INSERT INTO identity.users(id,display_name) SELECT md5('other-actor-'||g::text)::uuid,'Plan actor' FROM generate_series(1,5000) g`)
	exec(`INSERT INTO identity.memberships(tenant_id,user_id,roles) SELECT $1,md5('other-actor-'||g::text)::uuid,ARRAY['platform_viewer'] FROM generate_series(1,5000) g`, tenant)
	exec(`ANALYZE identity.member_roles; ANALYZE identity.roles`)
	rows, err := s.DB.Query(ctx, `EXPLAIN (ANALYZE,BUFFERS) SELECT coalesce(array_agg(DISTINCT cap ORDER BY cap),'{}'::text[]) FROM identity.member_roles mr JOIN identity.roles r ON r.tenant_id=mr.tenant_id AND r.id=mr.role_id CROSS JOIN LATERAL unnest(r.capabilities) cap WHERE mr.tenant_id=$1 AND mr.user_id=$2`, tenant, user)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		t.Log(line)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
