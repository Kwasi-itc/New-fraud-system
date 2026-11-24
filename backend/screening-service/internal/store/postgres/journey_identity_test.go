package postgres_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/Kwasi-itc/New-fraud-system/backend/platform-auth/authtest"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Process journeys share public trust, while each child receives only its own
// private leaf. These ephemeral keys exist only inside this test process.
var journeyIdentities struct {
	sync.Mutex
	services map[string]*auth.Signer
	human    *rsa.PrivateKey
}

func journeySigner(t testing.TB, service string) *auth.Signer {
	t.Helper()
	journeyIdentities.Lock()
	defer journeyIdentities.Unlock()
	if journeyIdentities.services == nil {
		journeyIdentities.services = map[string]*auth.Signer{}
	}
	if signer := journeyIdentities.services[service]; signer != nil {
		return signer
	}
	signer := authtest.ServiceSigner(t, service)
	journeyIdentities.services[service] = signer
	return signer
}

func journeyHumanKey(t testing.TB) *rsa.PrivateKey {
	t.Helper()
	journeyIdentities.Lock()
	defer journeyIdentities.Unlock()
	if journeyIdentities.human == nil {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		journeyIdentities.human = key
	}
	return journeyIdentities.human
}

func journeyService(t testing.TB, binary string, worker bool) string {
	t.Helper()
	for env, service := range map[string]string{
		"CASE_MANAGER_INTEGRATION_BINARY":     "case-manager-service",
		"DECISION_INTEGRATION_BINARY":         "decision-engine-service",
		"SCREENING_INTEGRATION_BINARY":        "screening-service",
		"DECISION_WORKER_INTEGRATION_BINARY":  "decision-engine-worker",
		"SCREENING_WORKER_INTEGRATION_BINARY": "screening-worker",
	} {
		if value := os.Getenv(env); value != "" && value == binary {
			return service
		}
	}
	for _, service := range []string{"case-manager", "decision-engine", "screening"} {
		if strings.Contains(filepath.Base(binary), service) {
			if worker {
				return service + "-worker"
			}
			return service + "-service"
		}
	}
	t.Fatalf("unknown journey binary identity: %s", binary)
	return ""
}

func journeyEnvironment(t testing.TB, binary string, worker bool) []string {
	t.Helper()
	service := journeyService(t, binary, worker)
	keys := []auth.TrustedKey{}
	for _, name := range []string{"case-manager-service", "case-manager-worker", "decision-engine-service", "decision-engine-worker", "screening-service", "screening-worker", "screening-case-delivery-worker", "ingestion-service", "data-model-service"} {
		keys = append(keys, authtest.ServiceTrust(t, journeySigner(t, name)))
	}
	human := journeyHumanKey(t)
	pub := pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&human.PublicKey)})
	keys = append(keys, auth.TrustedKey{ID: "journey-human", PublicKey: string(pub), Issuer: "journey-identity", PrincipalTypes: []auth.PrincipalType{auth.PrincipalUser}, Audiences: []string{"urn:fraud:platform-api"}})
	data, err := json.Marshal(auth.KeyRing{Keys: keys})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	publicFile, privateFile := filepath.Join(dir, "public.json"), filepath.Join(dir, "private.pem")
	if err := os.WriteFile(publicFile, data, 0600); err != nil {
		t.Fatal(err)
	}
	signer := journeySigner(t, service)
	private, err := x509.MarshalPKCS8PrivateKey(signer.Key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(privateFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0600); err != nil {
		t.Fatal(err)
	}
	audience := service
	if worker {
		audience = strings.TrimSuffix(service, "-worker") + "-service"
	}
	return []string{"PLATFORM_AUTH_KEYS_FILE=" + publicFile, "PLATFORM_AUTH_AUDIENCE=urn:fraud:" + audience, "PLATFORM_AUTH_SIGNING_KEY_FILE=" + privateFile, "PLATFORM_AUTH_SIGNING_KEY_ID=" + signer.KeyID, "PLATFORM_AUTH_SERVICE_ID=" + service}
}

func journeySubject(subject string) string {
	if _, err := uuid.Parse(subject); err == nil {
		return subject
	}
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("screening-journey:"+subject)).String()
}

func journeyHuman(t testing.TB, db *pgxpool.Pool, tenant, subject, role string) string {
	t.Helper()
	subject = journeySubject(subject)
	sid := uuid.NewSHA1(uuid.MustParse(tenant), []byte(subject+":session"))
	ctx := context.Background()
	var exists bool
	if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.tenants WHERE id=$1)`, tenant).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		for _, statement := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO identity.users(id,display_name) VALUES($1,'Journey actor') ON CONFLICT DO NOTHING`, []any{subject}},
			{`INSERT INTO identity.memberships(user_id,tenant_id,roles) VALUES($1,$2,ARRAY[$3]::text[]) ON CONFLICT DO NOTHING`, []any{subject, tenant, role}},
			{`INSERT INTO identity.sessions(id,user_id,tenant_id,digest,expires_at,idle_until) VALUES($1,$2,$3,convert_to($1::uuid::text,'UTF8'),now()+interval '1 hour',now()+interval '1 hour') ON CONFLICT DO NOTHING`, []any{sid, subject, tenant}},
		} {
			if _, err := db.Exec(ctx, statement.sql, statement.args...); err != nil {
				t.Fatal(err)
			}
		}
	}
	return journeySignHuman(t, auth.Principal{Subject: subject, TenantID: uuid.MustParse(tenant), SessionID: sid, AuthzVersion: 1}, role)
}

func journeySignHuman(t testing.TB, p auth.Principal, roles ...string) string {
	t.Helper()
	caps := []string{}
	for _, role := range roles {
		caps = append(caps, auth.PredefinedRoles()[role]...)
	}
	slices.Sort(caps)
	caps = slices.Compact(caps)
	// Unknown roles represent a valid identity with no endpoint authority.
	if len(caps) == 0 {
		caps = []string{"services:read"}
	}
	now := time.Now()
	claims := auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: "journey-identity", Subject: p.Subject, Audience: jwt.ClaimStrings{"urn:fraud:platform-api"}, ID: uuid.NewString(), IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute))}, PrincipalType: auth.PrincipalUser, TenantID: p.TenantID, SessionID: p.SessionID, AuthzVersion: p.AuthzVersion, Roles: roles, Capabilities: caps}
	raw, err := auth.SignAccessToken(journeyHumanKey(t), "journey-human", claims)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func journeyToken(t testing.TB, tenant, service, receiver, capability string) string {
	t.Helper()
	raw, err := journeySigner(t, service).Sign(uuid.MustParse(tenant), "urn:fraud:"+receiver, []string{capability}, auth.Principal{})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func journeyVerifyRequest(t testing.TB, r *http.Request, service, receiver, capability string) bool {
	t.Helper()
	v := authtest.ServiceVerifier(t, journeySigner(t, service), "urn:fraud:"+receiver)
	p, err := v.Verify(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	return err == nil && p.Can(capability)
}
