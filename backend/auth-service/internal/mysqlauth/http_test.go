package mysqlauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/internal/oidcprovider"
	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/internal/platform"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// This is an isolated protocol fixture, not a deployed identity provider. It
// signs actual ID tokens and checks confidential-client PKCE/code single use.
func oidcFixture(t *testing.T) (*oidcprovider.Provider, string, func(string, oidcprovider.Identity) string) {
	t.Helper()
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	type code struct {
		nonce, challenge string
		identity         oidcprovider.Identity
	}
	codes := map[string]code{}
	var mu sync.Mutex
	var issuer string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}, "code_challenge_methods_supported": []string{"S256"}})
		case "/keys":
			json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "native", Algorithm: "RS256", Use: "sig"}}})
		case "/token":
			if e := r.ParseForm(); e != nil {
				http.Error(w, "invalid", 400)
				return
			}
			client, secret, ok := r.BasicAuth()
			if !ok || client != "auth-fixture" || secret != "fixture-private-client" {
				http.Error(w, "denied", 401)
				return
			}
			mu.Lock()
			c, ok := codes[r.Form.Get("code")]
			delete(codes, r.Form.Get("code"))
			mu.Unlock()
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != c.challenge || r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("redirect_uri") != "http://localhost:3000/api/auth/callback" {
				http.Error(w, "invalid code or PKCE", 400)
				return
			}
			claims := jwt.MapClaims{"iss": issuer, "aud": "auth-fixture", "sub": c.identity.Subject, "name": c.identity.Name, "email": c.identity.Email, "email_verified": c.identity.EmailVerified, "nonce": c.nonce, "iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix()}
			token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
			token.Header["kid"] = "native"
			signed, e := token.SignedString(key)
			if e != nil {
				http.Error(w, "signing", 500)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "fixture-access", "token_type": "Bearer", "expires_in": 60, "id_token": signed})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)
	issuer = upstream.URL
	provider, e := oidcprovider.New(context.Background(), oidcprovider.Config{Environment: "development", IssuerURL: issuer, ClientID: "auth-fixture", ClientSecret: "fixture-private-client", RedirectURL: "http://localhost:3000/api/auth/callback"})
	if e != nil {
		t.Fatal(e)
	}
	issue := func(location string, identity oidcprovider.Identity) string {
		u, e := url.Parse(location)
		if e != nil {
			t.Fatal(e)
		}
		q := u.Query()
		if q.Get("state") == "" || q.Get("nonce") == "" || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
			t.Fatal("missing OIDC state/PKCE")
		}
		value := uuid.NewString()
		mu.Lock()
		codes[value] = code{q.Get("nonce"), q.Get("code_challenge"), identity}
		mu.Unlock()
		return value
	}
	return provider, issuer, issue
}

func TestMySQLHTTPAllFourteenOperationsAndDocs(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	provider, issuer, issue := oidcFixture(t)
	if _, e := f.store.Provision(ctx, f.tenant, issuer, "http-admin", "HTTP Admin", []string{"tenant_admin", "platform_admin"}); e != nil {
		t.Fatal(e)
	}
	inv, e := NewInvitations(f.store, issuer, "http://localhost:3000", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if e != nil {
		t.Fatal(e)
	}
	f.invites = inv
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	proxy := strings.Repeat("p", 64)
	app := platform.Server{Store: f.store, DefaultTenantID: f.tenant, Provider: provider, ProxyToken: proxy, Key: key, KeyID: "native-platform", Issuer: "https://platform.test", Audience: "fraud", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Invitations: inv, ConfigureMux: ConfigureDocs}
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	covered := map[string]bool{}
	call := func(method, path string, body any, want int, authenticated bool) map[string]any {
		t.Helper()
		var reader io.Reader
		if body != nil {
			b, e := json.Marshal(body)
			if e != nil {
				t.Fatal(e)
			}
			reader = bytes.NewReader(b)
		}
		request, e := http.NewRequest(method, server.URL+path, reader)
		if e != nil {
			t.Fatal(e)
		}
		request.Header.Set("Content-Type", "application/json")
		if authenticated {
			request.Header.Set("Authorization", "Bearer "+proxy)
		}
		response, e := server.Client().Do(request)
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		b, e := io.ReadAll(response.Body)
		if e != nil {
			t.Fatal(e)
		}
		if response.StatusCode != want {
			t.Fatalf("%s %s: status %d want %d; %s", method, path, response.StatusCode, want, b)
		}
		if want >= 200 && want < 300 {
			covered[method+" "+path] = true
			t.Logf("AUTH_ROUTE_PASS %s %s %d", method, path, want)
		}
		out := map[string]any{}
		if len(b) > 0 && response.Header.Get("Content-Type") == "application/json" {
			if e = json.Unmarshal(b, &out); e != nil {
				t.Fatal(e)
			}
		}
		return out
	}
	call("GET", "/healthz", nil, 204, false)
	call("GET", "/readyz", nil, 204, false)
	spec := call("GET", "/openapi.json", nil, 200, false)
	if spec["openapi"] != "3.0.3" || len(spec["paths"].(map[string]any)) != 14 {
		t.Fatal("invalid OpenAPI inventory")
	}
	call("GET", "/docs", nil, 200, false)
	call("GET", "/capabilities", nil, 200, false)
	for _, op := range operations {
		if op.Method == "POST" {
			call(op.Method, op.Path, map[string]any{}, 401, false)
		}
	}
	login := call("POST", "/internal/login/start", nil, 200, true)
	location := login["url"].(string)
	u, _ := url.Parse(location)
	code := issue(location, oidcprovider.Identity{Subject: "http-admin", Name: "HTTP Admin"})
	complete := map[string]any{"state": u.Query().Get("state"), "binding": login["binding"], "code": code}
	raw := call("POST", "/internal/login/complete", complete, 200, true)["session"].(string)
	call("POST", "/internal/login/complete", complete, 403, true)
	resolved := call("POST", "/internal/session", map[string]any{"session": raw}, 200, true)
	signed := resolved["access_token"].(string)
	token, e := jwt.Parse(signed, func(token *jwt.Token) (any, error) { return &key.PublicKey, nil }, jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer("https://platform.test"), jwt.WithAudience("fraud"))
	if e != nil || !token.Valid {
		t.Fatal("invalid platform token", e)
	}
	call("POST", "/internal/session", map[string]any{"session": "forged"}, 401, true)
	sessions := call("POST", "/internal/sessions/list", map[string]any{"session": raw}, 200, true)
	if len(sessions["items"].([]any)) != 1 {
		t.Fatal("session inventory")
	}
	other, e := f.store.CreateSessionForTenant(ctx, issuer, "http-admin", f.tenant)
	if e != nil {
		t.Fatal(e)
	}
	otherSession, e := f.store.Resolve(ctx, other)
	if e != nil {
		t.Fatal(e)
	}
	call("POST", "/internal/sessions/revoke", map[string]any{"session": raw, "target": otherSession.ID}, 200, true)
	call("POST", "/internal/session", map[string]any{"session": other}, 401, true)
	call("POST", "/internal/members/list", map[string]any{"session": raw}, 200, true)
	viewer, e := f.store.Provision(ctx, f.tenant, issuer, "http-viewer", "HTTP Viewer", []string{"platform_viewer"})
	if e != nil {
		t.Fatal(e)
	}
	call("POST", "/internal/members/change", map[string]any{"session": raw, "user_id": viewer, "version": 1, "roles": []string{"platform_builder"}, "active": true}, 204, true)
	call("POST", "/internal/members/change", map[string]any{"session": raw, "user_id": viewer, "version": 1, "roles": []string{"platform_viewer"}, "active": true}, 409, true)
	invite := uuid.New()
	call("POST", "/internal/invitations/change", map[string]any{"session": raw, "action": "create", "id": invite, "email": "HTTP@example.test", "roles": []string{"platform_viewer"}}, 204, true)
	invToken := invitationToken(t, f, invite)
	invLogin := call("POST", "/internal/invitations/start", map[string]any{"token": invToken}, 200, true)
	invLocation := invLogin["url"].(string)
	invURL, _ := url.Parse(invLocation)
	invCode := issue(invLocation, oidcprovider.Identity{Subject: "http-invited", Name: "HTTP Invited", Email: "HTTP@example.test", EmailVerified: true})
	invRaw := call("POST", "/internal/login/complete", map[string]any{"state": invURL.Query().Get("state"), "binding": invLogin["binding"], "code": invCode}, 200, true)["session"].(string)
	call("POST", "/internal/members/list", map[string]any{"session": invRaw}, 403, true)
	call("POST", "/internal/invitations/list", map[string]any{"session": raw}, 200, true)
	call("POST", "/internal/audit/list", map[string]any{"session": raw}, 200, true)
	call("POST", "/internal/logout", map[string]any{"session": raw}, 204, true)
	call("POST", "/internal/session", map[string]any{"session": raw}, 401, true)
	for _, op := range operations {
		if !covered[op.Method+" "+op.Path] {
			t.Errorf("missing positive HTTP assertion: %s %s", op.Method, op.Path)
		}
	}
	f.db.Close()
	call("GET", "/healthz", nil, 204, false)
	call("GET", "/readyz", nil, 503, false)
}
