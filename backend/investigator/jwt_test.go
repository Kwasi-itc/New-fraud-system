package investigator

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestPlatformSessionClaims(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewVerifier(map[string][]byte{"key": pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub})}, "platform", "case-manager")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("USER_REQUIRE_SESSION", "true")
	claims := Claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: "platform", Subject: uuid.NewString(), Audience: []string{"case-manager"}, IssuedAt: jwt.NewNumericDate(time.Now()), ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute))}, TenantID: uuid.NewString(), Roles: []string{"case_investigator"}}
	signed := func(c Claims) string {
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, c)
		token.Header["kid"] = "key"
		raw, err := token.SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	if _, err = v.Verify(signed(claims)); err == nil {
		t.Fatal("legacy JWT bypassed required session")
	}
	claims.SessionID = uuid.NewString()
	claims.AuthzVersion = 1
	p, err := v.Verify(signed(claims))
	if err != nil || p.SessionID == uuid.Nil || p.AuthzVersion != 1 || p.Admin {
		t.Fatal("valid investigator rejected", err)
	}
	claims.Roles = []string{"tenant_admin"}
	if _, err = v.VerifyPlatformSession(signed(claims)); err != nil {
		t.Fatal("identity-only verification imposed a case role", err)
	}
	if _, err = v.Verify(signed(claims)); err == nil {
		t.Fatal("identity admin became case admin")
	}
	claims.Roles = []string{"case_manager_admin"}
	p, err = v.Verify(signed(claims))
	if err != nil || !p.Admin {
		t.Fatal("case admin rejected", err)
	}
	claims.AuthzVersion = 0
	if _, err = v.VerifyPlatformSession(signed(claims)); err == nil {
		t.Fatal("platform accepted missing version")
	}
	if _, err = v.Verify(signed(claims)); err == nil {
		t.Fatal("missing permission version accepted")
	}
	claims.AuthzVersion = 1
	claims.SessionID = ""
	t.Setenv("USER_REQUIRE_SESSION", "false")
	if _, err = v.VerifyPlatformSession(signed(claims)); err == nil {
		t.Fatal("platform accepted a legacy sessionless token")
	}
	claims.SessionID = uuid.NewString()
	claims.Subject = "legacy-actor"
	if _, err = v.VerifyPlatformSession(signed(claims)); err == nil {
		t.Fatal("platform accepted a non-UUID actor")
	}
}
