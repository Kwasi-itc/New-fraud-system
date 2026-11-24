package authtest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"testing"
	"time"
)

// HumanIdentity issues a real typed token for HTTP contract tests. Domain-only
// tests can instead install a test principal directly on their context.
func HumanIdentity(t testing.TB, tenant uuid.UUID, capabilities ...string) (*auth.Verifier, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pub := pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey)})
	v, err := auth.NewVerifier(map[string][]byte{"human-test": pub}, "test-identity", "urn:fraud:platform-api")
	if err != nil {
		t.Fatal(err)
	}
	claims := auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: "test-identity", Subject: uuid.NewString(), Audience: jwt.ClaimStrings{"urn:fraud:platform-api"}, ID: uuid.NewString(), IssuedAt: jwt.NewNumericDate(time.Now()), ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute))}, PrincipalType: auth.PrincipalUser, TenantID: tenant, SessionID: uuid.New(), AuthzVersion: 1, Capabilities: capabilities}
	claims.Roles = []string{"role_" + uuid.NewString()}
	for _, cap := range capabilities {
		if cap == "roles:manage" || cap == "roles:assign" {
			claims.Roles = []string{"tenant_admin"}
		}
	}
	raw, err := auth.SignAccessToken(key, "human-test", claims)
	if err != nil {
		t.Fatal(err)
	}
	return v, raw
}

func ClientIdentity(t testing.TB, tenant, client uuid.UUID, scopes ...string) (*auth.Verifier, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pub := pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey)})
	v, err := auth.NewTrustedVerifier(auth.KeyRing{Keys: []auth.TrustedKey{{ID: "client-test", PublicKey: string(pub), Issuer: "test-identity", PrincipalTypes: []auth.PrincipalType{auth.PrincipalClient}, Audiences: []string{auth.ClientAudience}}}}, auth.ClientAudience)
	if err != nil {
		t.Fatal(err)
	}
	claims := auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: "test-identity", Subject: client.String(), Audience: jwt.ClaimStrings{auth.ClientAudience}, ID: uuid.NewString(), IssuedAt: jwt.NewNumericDate(time.Now()), ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute))}, PrincipalType: auth.PrincipalClient, TenantID: tenant, ClientID: client.String(), Scopes: scopes}
	raw, err := auth.SignAccessToken(key, "client-test", claims)
	if err != nil {
		t.Fatal(err)
	}
	return v, raw
}
