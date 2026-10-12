package oidcprovider

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

func TestConfigRejectsUnsafeURLs(t *testing.T) {
	base := Config{Environment: "production", IssuerURL: "https://id.example/realm", ClientID: "app", ClientSecret: "private", RedirectURL: "https://app.example/callback"}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, edit := range []func(*Config){
		func(c *Config) { c.Environment = "" }, func(c *Config) { c.IssuerURL = "http://id.example" },
		func(c *Config) { c.RedirectURL = "https://app.example/callback?return=evil" },
		func(c *Config) { c.IssuerURL = "https://user:secret@id.example" },
		func(c *Config) { c.BackchannelOrigin = "https://internal.example/path" },
		func(c *Config) { c.BackchannelOrigin = "http://internal.example" },
		func(c *Config) { c.ClientSecret = "" },
	} {
		c := base
		edit(&c)
		if c.Validate() == nil {
			t.Fatal("unsafe config accepted")
		}
	}
}

func TestOIDCExchangeAndProviderSwap(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	for _, realm := range []string{"local-fraud", "replacement-company"} {
		t.Run(realm, func(t *testing.T) {
			var issuer string
			var mutate func(map[string]any)
			var tokenMode string
			nonce := strings.Repeat("n", 32)
			verifier := strings.Repeat("v", 43)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/" + realm + "/.well-known/openid-configuration":
					_ = json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}, "code_challenge_methods_supported": []string{"S256"}})
				case "/" + realm + "/keys":
					_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test", Algorithm: "RS256", Use: "sig"}}})
				case "/" + realm + "/token":
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					id, secret, ok := r.BasicAuth()
					if !ok || id != "app" || secret != "private" || r.Form.Get("code_verifier") != verifier || r.Form.Get("redirect_uri") != "http://localhost:3000/api/auth/callback" || r.Form.Get("grant_type") != "authorization_code" {
						t.Error("incorrect confidential PKCE exchange")
					}
					if tokenMode == "error" {
						http.Error(w, "sensitive provider diagnostic", 400)
						return
					}
					claims := map[string]any{"iss": issuer, "sub": "external-operator", "aud": "app", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix(), "nonce": nonce, "email": "operator@example.test", "email_verified": true, "name": "Operator"}
					if mutate != nil {
						mutate(claims)
					}
					payload, _ := json.Marshal(claims)
					signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "test"))
					if err != nil {
						t.Error(err)
						return
					}
					signed, err := signer.Sign(payload)
					if err != nil {
						t.Error(err)
						return
					}
					raw, err := signed.CompactSerialize()
					if err != nil {
						t.Error(err)
						return
					}
					if tokenMode == "forged" {
						parts := strings.Split(raw, ".")
						parts[2] = strings.Repeat("A", len(parts[2]))
						raw = strings.Join(parts, ".")
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "upstream-only", "token_type": "Bearer", "expires_in": 60, "id_token": raw})
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			// Public issuer is deliberately unreachable. All server calls use the
			// configured transport origin while signatures retain the public issuer.
			issuer = "http://browser.invalid/" + realm
			p, err := New(context.Background(), Config{Environment: "development", IssuerURL: issuer, BackchannelOrigin: server.URL, ClientID: "app", ClientSecret: "private", RedirectURL: "http://localhost:3000/api/auth/callback"})
			if err != nil {
				t.Fatal(err)
			}
			authURL, err := p.AuthorizationURL(strings.Repeat("s", 32), nonce, verifier)
			if err != nil {
				t.Fatal(err)
			}
			u, _ := url.Parse(authURL)
			if u.Host != "browser.invalid" || u.Query().Get("response_type") != "code" || u.Query().Get("code_challenge_method") != "S256" || u.Query().Get("nonce") != nonce || u.Query().Get("client_secret") != "" {
				t.Fatal("unsafe authorization URL")
			}
			if _, err = p.AuthorizationURL("short", nonce, verifier); err == nil {
				t.Fatal("weak transaction input accepted")
			}
			identity, err := p.Exchange(context.Background(), "test-code", verifier, nonce)
			if err != nil || identity.Issuer != issuer || identity.Subject != "external-operator" || !identity.EmailVerified {
				t.Fatalf("identity: %+v %v", identity, err)
			}
			for _, tc := range []struct {
				name string
				edit func(map[string]any)
			}{
				{"nonce", func(c map[string]any) { c["nonce"] = "wrong" }},
				{"issuer", func(c map[string]any) { c["iss"] = "https://other.example" }},
				{"audience", func(c map[string]any) { c["aud"] = "other-client" }},
				{"expired", func(c map[string]any) { c["exp"] = time.Now().Add(-time.Hour).Unix() }},
				{"azp", func(c map[string]any) { c["azp"] = "other-client" }},
				{"multi-audience", func(c map[string]any) { c["aud"] = []string{"app", "other"} }},
				{"missing-issued", func(c map[string]any) { delete(c, "iat") }},
				{"future-issued", func(c map[string]any) { c["iat"] = time.Now().Add(time.Hour).Unix() }},
				{"empty-subject", func(c map[string]any) { c["sub"] = "" }},
			} {
				t.Run(tc.name, func(t *testing.T) {
					mutate = tc.edit
					if _, err := p.Exchange(context.Background(), "test-code", verifier, nonce); err == nil {
						t.Fatal("invalid identity accepted")
					}
				})
			}
			mutate = nil
			for _, mode := range []string{"error", "forged"} {
				tokenMode = mode
				if _, err := p.Exchange(context.Background(), "test-code", verifier, nonce); err == nil || strings.Contains(err.Error(), "sensitive") {
					t.Fatal("unsafe exchange failure")
				}
			}
		})
	}
}

func TestDiscoveryFailsClosed(t *testing.T) {
	for _, mode := range []string{"wrong-issuer", "cross-origin", "no-pkce", "too-large", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			var origin string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "too-large" {
					_, _ = w.Write([]byte(strings.Repeat("x", (1<<20)+1)))
					return
				}
				if mode == "redirect" {
					http.Redirect(w, r, "https://outside.example", 302)
					return
				}
				issuer, token := origin, origin+"/token"
				methods := []string{"S256"}
				if mode == "wrong-issuer" {
					issuer = "https://outside.example"
				}
				if mode == "cross-origin" {
					token = "https://outside.example/token"
				}
				if mode == "no-pkce" {
					methods = nil
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": origin + "/authorize", "token_endpoint": token, "jwks_uri": origin + "/keys", "code_challenge_methods_supported": methods})
			}))
			defer server.Close()
			origin = server.URL
			if _, err := New(context.Background(), Config{Environment: "development", IssuerURL: origin, ClientID: "app", ClientSecret: "private", RedirectURL: "http://localhost/callback"}); err == nil {
				t.Fatal("unsafe metadata accepted")
			}
		})
	}
}
