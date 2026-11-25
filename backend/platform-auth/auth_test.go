package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func testKey(t *testing.T) (*rsa.PrivateKey, string) {
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
func userClaims() Claims {
	now := time.Now()
	return Claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: PlatformIssuer, Subject: uuid.NewString(), Audience: jwt.ClaimStrings{PlatformAudience}, ID: uuid.NewString(), IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(UserTTL))}, PrincipalType: PrincipalUser, TenantID: uuid.New(), SessionID: uuid.New(), AuthzVersion: 1, Roles: []string{"case_investigator"}, Capabilities: []string{"cases:read"}}
}

func TestPlatformSignerStartupRejectsMismatchedTrust(t *testing.T) {
	key, pub := testKey(t)
	other, _ := testKey(t)
	policy := TrustedKey{ID: "platform", PublicKey: pub, Issuer: PlatformIssuer, PrincipalTypes: []PrincipalType{PrincipalUser, PrincipalClient}, Audiences: []string{PlatformAudience, ClientAudience}}
	v, err := NewTrustedVerifier(KeyRing{Keys: []TrustedKey{policy}}, PlatformAudience)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.ValidatePlatformSigner(key, "platform", PlatformIssuer, PlatformAudience, ClientAudience); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		key                  *rsa.PrivateKey
		id, issuer, audience string
	}{
		{other, "platform", PlatformIssuer, PlatformAudience},
		{key, "missing", PlatformIssuer, PlatformAudience},
		{key, "platform", "wrong-issuer", PlatformAudience},
		{key, "platform", PlatformIssuer, "wrong-audience"},
	} {
		if err := v.ValidatePlatformSigner(check.key, check.id, check.issuer, check.audience); err == nil {
			t.Fatal("invalid platform signer configuration accepted")
		}
	}
	policy.PrincipalTypes = []PrincipalType{PrincipalUser}
	v, err = NewTrustedVerifier(KeyRing{Keys: []TrustedKey{policy}}, PlatformAudience)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.ValidatePlatformSigner(key, "platform", PlatformIssuer, PlatformAudience); err == nil {
		t.Fatal("issuer missing client trust accepted")
	}
}
func TestVerifierRejectsTokenSubstitutionAndUnboundedLifetimes(t *testing.T) {
	key, pub := testKey(t)
	service := TrustedKey{ID: "worker", PublicKey: pub, Issuer: "urn:fraud:service:worker", ServiceID: "worker", PrincipalTypes: []PrincipalType{PrincipalService}, Capabilities: []string{"cases:events-write"}, Audiences: []string{"case-manager"}}
	platform := TrustedKey{ID: "platform", PublicKey: pub, Issuer: PlatformIssuer, PrincipalTypes: []PrincipalType{PrincipalUser, PrincipalClient}, Audiences: []string{PlatformAudience, "ingestion"}}
	v, err := NewTrustedVerifier(KeyRing{[]TrustedKey{platform, service}}, PlatformAudience, "case-manager")
	if err != nil {
		t.Fatal(err)
	}
	good := userClaims()
	raw, err := SignAccessToken(key, "platform", good)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = v.Verify(raw); err != nil {
		t.Fatal("valid human denied", err)
	}
	tests := map[string]func(*Claims){
		"wrong issuer":       func(c *Claims) { c.Issuer = "attacker" },
		"wrong audience":     func(c *Claims) { c.Audience = jwt.ClaimStrings{"ingestion"} },
		"multiple audiences": func(c *Claims) { c.Audience = append(c.Audience, "case-manager") },
		"no session":         func(c *Claims) { c.SessionID = uuid.Nil },
		"no tenant":          func(c *Claims) { c.TenantID = uuid.Nil },
		"long lifetime":      func(c *Claims) { c.ExpiresAt = jwt.NewNumericDate(c.IssuedAt.Add(2 * time.Minute)) },
		"machine role admin": func(c *Claims) {
			c.PrincipalType = PrincipalClient
			c.ClientID = c.Subject
			c.Scopes = []string{"roles:manage"}
			c.Capabilities = nil
			c.SessionID = uuid.Nil
			c.AuthzVersion = 0
		},
		"missing role":             func(c *Claims) { c.Roles = nil },
		"multiple roles":           func(c *Claims) { c.Roles = []string{"case_investigator", "platform_viewer"} },
		"reserved without builtin": func(c *Claims) { c.Capabilities = []string{"roles:manage"}; c.Roles = []string{"custom-admin"} },
		"unknown capability":       func(c *Claims) { c.Capabilities = []string{"*"} },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			c := good
			change(&c)
			raw, e := SignAccessToken(key, "platform", c)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = v.Verify(raw); e == nil {
				t.Fatal("invalid token accepted")
			}
		})
	}
	if raw, err = SignAccessToken(key, "worker", good); err != nil {
		t.Fatal(err)
	}
	if _, err = v.Verify(raw); err == nil {
		t.Fatal("service key minted human identity")
	}
	s := Signer{key, "worker", "worker"}
	raw, err = s.Sign(good.TenantID, "case-manager", []string{"cases:events-write"}, Principal{Type: PrincipalUser, Subject: good.Subject, TenantID: good.TenantID})
	if err != nil {
		t.Fatal(err)
	}
	p, err := v.Verify(raw)
	if err != nil || p.OrigID != good.Subject || p.Type != PrincipalService {
		t.Fatal("service provenance", p, err)
	}
	raw, err = s.Sign(good.TenantID, "case-manager", []string{"workflows:execute"}, Principal{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = v.Verify(raw); err == nil {
		t.Fatal("worker exceeded trusted capabilities")
	}
}
func TestExplicitRoleBundlesAndReservedGrants(t *testing.T) {
	roles := PredefinedRoles()
	for _, c := range Catalog() {
		if c.Human && !slices.Contains(roles["tenant_admin"], c.Name) {
			t.Fatalf("tenant admin missing %s", c.Name)
		}
		if !c.Human && slices.Contains(roles["tenant_admin"], c.Name) {
			t.Fatalf("human got service authority %s", c.Name)
		}
	}
	for name, caps := range roles {
		if err := ValidateCapabilities(caps, PrincipalUser, false); err != nil {
			t.Fatal(name, err)
		}
		if name != "tenant_admin" && (slices.Contains(caps, "roles:manage") || slices.Contains(caps, "roles:assign")) {
			t.Fatal("reserved grant leaked")
		}
	}
	if err := ValidateCapabilities([]string{"roles:manage"}, PrincipalUser, true); err == nil {
		t.Fatal("custom role can administer roles")
	}
}
func TestScenarioDraftClientCapabilitiesExcludePublication(t *testing.T) {
	if err := ValidateCapabilities([]string{"models:read", "scenarios:read", "scenarios:write"}, PrincipalClient, false); err != nil {
		t.Fatal("client draft-authoring scopes rejected", err)
	}
	if err := ValidateCapabilities([]string{"scenarios:publish"}, PrincipalClient, false); err == nil {
		t.Fatal("client can receive scenario publication scope")
	}
	if err := ValidateCapabilities([]string{"models:write"}, PrincipalClient, false); err == nil {
		t.Fatal("client can receive general model write scope")
	}
}
func TestReplayConsumptionIsAtomicAndCapacityFailsClosed(t *testing.T) {
	c := NewReplayCache(2)
	now := time.Now()
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if c.Consume("client", "proof", now.Add(time.Minute), now) == nil {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatal("replayed proof accepted", accepted.Load())
	}
	if err := c.Consume("other-client", "proof", now.Add(time.Minute), now); err != nil {
		t.Fatal("client collision", err)
	}
	if err := c.Consume("client", "new-proof", now.Add(time.Minute), now); err != ErrReplayCapacity {
		t.Fatal("cache evicted live replay state", err)
	}
	if err := c.Consume("client", "new-proof", now.Add(3*time.Minute), now.Add(2*time.Minute)); err != nil {
		t.Fatal("expired entries not reclaimed", err)
	}
}

type fixedVerifier struct{ p Principal }

func (v fixedVerifier) Verify(string) (Principal, error) { return v.p, nil }
func TestRoutesEnforceTenantPrincipalAndCoverage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenant := uuid.New()
	base := Principal{Type: PrincipalUser, Subject: uuid.NewString(), TenantID: tenant, Capabilities: map[string]bool{"cases:read": true}}
	for _, tc := range []struct {
		name   string
		p      Principal
		tenant uuid.UUID
		want   int
	}{
		{"own tenant", base, tenant, 200},
		{"foreign tenant", base, uuid.New(), 403},
		{"machine human route", Principal{Type: PrincipalClient, Subject: "bank", TenantID: tenant, Capabilities: base.Capabilities}, tenant, 403},
		{"missing capability", Principal{Type: PrincipalUser, Subject: "user", TenantID: tenant}, tenant, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := gin.New()
			r := NewRouter(e, fixedVerifier{tc.p})
			r.GET("/tenants/:tenantId/cases", Policy{Capability: "cases:read", PrincipalType: PrincipalUser}, func(c *gin.Context) { c.Status(200) })
			if err := r.ValidateCoverage(); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodGet, "/tenants/"+tc.tenant.String()+"/cases", nil)
			req.Header.Set("Authorization", "Bearer signed")
			w := httptest.NewRecorder()
			e.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	e := gin.New()
	r := NewRouter(e, fixedVerifier{base})
	e.GET("/forgotten", func(c *gin.Context) {})
	if err := r.ValidateCoverage(); err == nil {
		t.Fatal("unprotected route omitted from coverage")
	}
	if err := (Policy{}).Validate(); err == nil {
		t.Fatal("empty protected policy accepted")
	}
}

func BenchmarkRSA3072Verification(b *testing.B) {
	key, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		b.Fatal(err)
	}
	public := pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey)})
	v, err := NewVerifier(map[string][]byte{"benchmark": public}, "bench-identity", "urn:fraud:platform-api")
	if err != nil {
		b.Fatal(err)
	}
	claims := Claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: "bench-identity", Subject: uuid.NewString(), Audience: jwt.ClaimStrings{"urn:fraud:platform-api"}, ID: uuid.NewString(), IssuedAt: jwt.NewNumericDate(time.Now()), ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute))}, PrincipalType: PrincipalUser, TenantID: uuid.New(), SessionID: uuid.New(), AuthzVersion: 1, Roles: []string{"case_investigator"}, Capabilities: []string{"cases:read"}}
	token, err := SignAccessToken(key, "benchmark", claims)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := v.Verify(token); err != nil {
			b.Fatal(err)
		}
	}
}

func TestInvestigatorBundlesDoNotGrantRawScreeningAuthority(t *testing.T) {
	for _, name := range []string{"case_investigator", "case_manager_admin"} {
		caps := PredefinedRoles()[name]
		if slices.Contains(caps, "screening:read") || slices.Contains(caps, "screening:review") {
			t.Fatal("case role bypasses case-scoped inbox authority", name)
		}
		if !slices.Contains(caps, "cases:review") || !slices.Contains(caps, "screening:search") {
			t.Fatal("case investigation functionality lost", name)
		}
	}
	for _, cap := range []string{"ingest:write", "decisions:evaluate", "cases:read", "models:read"} {
		if RequiresCurrentAuthority(cap) {
			t.Fatal("standard operation gained a current-authority query", cap)
		}
	}
	for _, cap := range []string{"scenarios:publish", "screening:review", "roles:manage"} {
		if !RequiresCurrentAuthority(cap) {
			t.Fatal("protected mutation missing current-authority check", cap)
		}
	}
}
