package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestOneClientTokenAcrossAPIReceivers(t *testing.T) {
	key, public := testKey(t)
	serviceKey, servicePublic := testKey(t)
	legacyAudience := "urn:fraud:ingestion-service"
	ring := KeyRing{Keys: []TrustedKey{
		{ID: "platform", PublicKey: public, Issuer: PlatformIssuer, PrincipalTypes: []PrincipalType{PrincipalUser, PrincipalClient}, Audiences: []string{PlatformAudience, ClientAudience, legacyAudience}},
		// Deliberately permissive trust demonstrates that the system audience is
		// reserved for clients even if a service key policy is misconfigured.
		{ID: "worker", PublicKey: servicePublic, Issuer: "urn:fraud:service:worker", ServiceID: "worker", PrincipalTypes: []PrincipalType{PrincipalService}, Capabilities: []string{"ingest:write"}, Audiences: []string{legacyAudience, ClientAudience}},
	}}
	data, err := json.Marshal(ring)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "public.json")
	if err = os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PLATFORM_AUTH_KEYS_FILE", file)
	now := time.Now()
	client := uuid.NewString()
	claims := Claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: PlatformIssuer, Subject: client, Audience: jwt.ClaimStrings{ClientAudience}, ID: uuid.NewString(), IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(ClientTTL))}, PrincipalType: PrincipalClient, ClientID: client, TenantID: uuid.New(), Scopes: []string{"ingest:write", "decisions:evaluate", "decisions:read"}}
	raw, err := SignAccessToken(key, "platform", claims)
	if err != nil {
		t.Fatal(err)
	}
	for _, service := range []string{"ingestion-service", "decision-engine-service", "case-manager-service", "screening-service", "data-model-service", "future-api"} {
		t.Setenv("PLATFORM_AUTH_AUDIENCE", "urn:fraud:"+service)
		verifier, err := FromEnvironment()
		if err != nil {
			t.Fatal(err)
		}
		p, err := verifier.Verify(raw)
		if err != nil || !p.IsClient() || p.TenantID != claims.TenantID || !p.Can("ingest:write") || !p.Can("decisions:evaluate") || !p.Can("decisions:read") || p.Can("roles:manage") {
			t.Fatalf("%s rejected system-scoped client or expanded its grants: %v", service, err)
		}
	}
	t.Setenv("PLATFORM_AUTH_AUDIENCE", legacyAudience)
	verifier, err := FromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Claims){
		"legacy API audience":   func(c *Claims) { c.Audience = jwt.ClaimStrings{legacyAudience} },
		"human audience":        func(c *Claims) { c.Audience = jwt.ClaimStrings{PlatformAudience} },
		"multiple audiences":    func(c *Claims) { c.Audience = jwt.ClaimStrings{ClientAudience, legacyAudience} },
		"unknown audience":      func(c *Claims) { c.Audience = jwt.ClaimStrings{"urn:other:system"} },
		"human-only capability": func(c *Claims) { c.Scopes = []string{"roles:manage"} },
		"missing tenant":        func(c *Claims) { c.TenantID = uuid.Nil },
	} {
		t.Run(name, func(t *testing.T) {
			changed := claims
			mutate(&changed)
			bad, err := SignAccessToken(key, "platform", changed)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := verifier.Verify(bad); err == nil {
				t.Fatal("invalid client token accepted")
			}
		})
	}
	human := userClaims()
	human.Audience = jwt.ClaimStrings{ClientAudience}
	humanRaw, err := SignAccessToken(key, "platform", human)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.Verify(humanRaw); err == nil {
		t.Fatal("human authenticated with the client system audience")
	}
	signer := &Signer{Key: serviceKey, KeyID: "worker", ServiceID: "worker"}
	serviceRaw, err := signer.Sign(claims.TenantID, ClientAudience, []string{"ingest:write"}, Principal{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.Verify(serviceRaw); err == nil {
		t.Fatal("service authenticated with the client system audience")
	}
	confused, err := SignAccessToken(serviceKey, "worker", claims)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.Verify(confused); err == nil {
		t.Fatal("service key minted a client identity")
	}
}
