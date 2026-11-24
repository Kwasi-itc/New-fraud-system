package access

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestVerifiedIdentity(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	public := pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey)})
	v, err := NewVerifier(map[string][]byte{"current": public, "next": public}, "test-issuer", "case-manager")
	if err != nil {
		t.Fatal(err)
	}
	subject := uuid.NewString()
	base := func() Claims {
		return Claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: "test-issuer", Subject: subject, Audience: jwt.ClaimStrings{"case-manager"}, ID: uuid.NewString(), ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)), IssuedAt: jwt.NewNumericDate(time.Now())}, PrincipalType: auth.PrincipalUser, SessionID: uuid.New(), AuthzVersion: 1, Capabilities: []string{"cases:read"}, TenantID: uuid.New(), Roles: []string{"case_investigator"}}
	}
	for _, tt := range []struct {
		name   string
		change func(*Claims)
		kid    string
		valid  bool
	}{
		{"valid", func(*Claims) {}, "current", true},
		{"rotation", func(*Claims) {}, "next", true},
		{"expired", func(c *Claims) { c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour)) }, "current", false},
		{"missing expiration", func(c *Claims) { c.ExpiresAt = nil }, "current", false},
		{"future issued", func(c *Claims) { c.IssuedAt = jwt.NewNumericDate(time.Now().Add(time.Hour)) }, "current", false},
		{"missing issued", func(c *Claims) { c.IssuedAt = nil }, "current", false},
		{"future not before", func(c *Claims) { c.NotBefore = jwt.NewNumericDate(time.Now().Add(time.Hour)) }, "current", false},
		{"wrong issuer", func(c *Claims) { c.Issuer = "attacker" }, "current", false},
		{"wrong audience", func(c *Claims) { c.Audience = jwt.ClaimStrings{"other-service"} }, "current", false},
		{"role metadata optional", func(c *Claims) { c.Roles = nil }, "current", true},
		{"missing capability", func(c *Claims) { c.Capabilities = nil }, "current", false},
		{"missing subject", func(c *Claims) { c.Subject = "" }, "current", false},
		{"missing tenant", func(c *Claims) { c.TenantID = uuid.Nil }, "current", false},
		{"zero tenant", func(c *Claims) { c.TenantID = uuid.Nil }, "current", false},
		{"unknown key", func(*Claims) {}, "untrusted", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := base()
			tt.change(&c)
			token := jwt.NewWithClaims(jwt.SigningMethodRS256, c)
			token.Header["kid"] = tt.kid
			token.Header["typ"] = "at+jwt"
			raw, err := token.SignedString(key)
			if err != nil {
				t.Fatal(err)
			}
			p, err := v.Verify(raw)
			if (err == nil) != tt.valid {
				t.Fatalf("verification: %v", err)
			}
			if tt.valid && (p.Subject != subject || p.Kind != User || p.Admin) {
				t.Fatalf("wrong principal: %+v", p)
			}
		})
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, base())
	token.Header["kid"] = "current"
	raw, _ := token.SignedString(public)
	if _, err := v.Verify(raw); err == nil {
		t.Fatal("algorithm confusion accepted")
	}
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	token = jwt.NewWithClaims(jwt.SigningMethodRS256, base())
	token.Header["kid"] = "current"
	raw, _ = token.SignedString(other)
	if _, err := v.Verify(raw); err == nil {
		t.Fatal("wrong signature accepted")
	}
	if _, err := (*Verifier)(nil).Verify(raw); err == nil {
		t.Fatal("unconfigured verifier accepted token")
	}
}

func TestTenantBoundary(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	for _, p := range []Principal{{Kind: User, Subject: "u", TenantID: a}, {Kind: Service, Subject: "s", TenantID: a}} {
		ctx := WithPrincipal(context.Background(), p)
		if _, err := Tenant(ctx, a); err != nil {
			t.Fatal(err)
		}
		if _, err := Tenant(ctx, b); err == nil {
			t.Fatal("cross-tenant access")
		}
	}
	if _, err := Tenant(context.Background(), a); err == nil {
		t.Fatal("missing principal accepted")
	}
	if err := Administrator(WithPrincipal(context.Background(), Principal{Kind: Service, Subject: "s", Admin: true, TenantID: a}), a); err == nil {
		t.Fatal("service bypassed admin boundary")
	}
}
