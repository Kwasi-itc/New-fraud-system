// Package oidcprovider isolates the upstream OIDC protocol from platform users,
// roles and sessions. No Keycloak-specific claim grants application authority.
package oidcprovider

import (
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type Config struct {
	Environment       string
	IssuerURL         string
	BackchannelOrigin string
	ClientID          string
	ClientSecret      string
	RedirectURL       string
}

func FromEnvironment(get func(string) string) Config {
	return Config{Environment: get("AUTH_ENV"), IssuerURL: get("OIDC_ISSUER_URL"),
		BackchannelOrigin: get("OIDC_BACKCHANNEL_ORIGIN"), ClientID: get("OIDC_CLIENT_ID"),
		ClientSecret: get("OIDC_CLIENT_SECRET"), RedirectURL: get("OIDC_REDIRECT_URL")}
}

func configuredURL(raw string, development bool) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.Opaque != "" ||
		(u.Scheme != "https" && !(development && u.Scheme == "http")) {
		return nil, errors.New("OIDC URLs must be absolute HTTPS without credentials, query or fragment; HTTP requires AUTH_ENV=development")
	}
	return u, nil
}

func (c Config) Validate() error {
	if c.Environment != "development" && c.Environment != "production" {
		return errors.New("AUTH_ENV must be development or production")
	}
	for _, raw := range []string{c.IssuerURL, c.RedirectURL} {
		if _, err := configuredURL(raw, c.Environment == "development"); err != nil {
			return err
		}
	}
	if c.BackchannelOrigin != "" {
		u, err := configuredURL(c.BackchannelOrigin, c.Environment == "development")
		if err != nil {
			return err
		}
		if u.Path != "" && u.Path != "/" {
			return errors.New("OIDC_BACKCHANNEL_ORIGIN must not include a path")
		}
	}
	if strings.TrimSpace(c.ClientID) == "" || strings.TrimSpace(c.ClientSecret) == "" {
		return errors.New("OIDC client ID and confidential client secret are required")
	}
	return nil
}

type Provider struct {
	issuer   string
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
	client   *http.Client
}

// boundaryTransport allows only the configured issuer origin. The optional
// transport rewrite changes connectivity, NEVER the issuer asserted by tokens.
// It supports localhost (browser) versus service DNS (Docker) without disabling
// issuer validation. Cross-origin discovery endpoints fail closed for now.
type boundaryTransport struct {
	issuer, backchannel *url.URL
	base                http.RoundTripper
}

func (t boundaryTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != t.issuer.Scheme || r.URL.Host != t.issuer.Host || r.URL.User != nil {
		return nil, errors.New("OIDC endpoint is outside configured issuer origin")
	}
	clone := r.Clone(r.Context())
	u := *r.URL
	clone.URL = &u
	if t.backchannel != nil {
		clone.URL.Scheme = t.backchannel.Scheme
		clone.URL.Host = t.backchannel.Host
		clone.Host = t.backchannel.Host
	}
	resp, err := t.base.RoundTrip(clone)
	if err != nil {
		return nil, errors.New("OIDC transport unavailable")
	}
	defer resp.Body.Close()
	const maximum = 1 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maximum+1))
	if err != nil || len(body) > maximum {
		return nil, errors.New("OIDC response unreadable or too large")
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return resp, nil
}

func New(ctx context.Context, c Config) (*Provider, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	issuer, _ := url.Parse(c.IssuerURL)
	var backchannel *url.URL
	if c.BackchannelOrigin != "" {
		backchannel, _ = url.Parse(c.BackchannelOrigin)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 4
	transport.MaxIdleConnsPerHost = 2
	transport.MaxConnsPerHost = 4
	transport.ResponseHeaderTimeout = 10 * time.Second
	client := &http.Client{Timeout: 10 * time.Second, Transport: boundaryTransport{issuer, backchannel, transport},
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("OIDC redirects are not allowed") }}
	p, err := oidc.NewProvider(oidc.ClientContext(ctx, client), c.IssuerURL)
	if err != nil {
		return nil, errors.New("OIDC discovery failed; check issuer, connectivity and provider metadata")
	}
	var metadata struct {
		JWKS    string   `json:"jwks_uri"`
		Methods []string `json:"code_challenge_methods_supported"`
	}
	if p.Claims(&metadata) != nil {
		return nil, errors.New("invalid OIDC metadata")
	}
	s256 := false
	for _, m := range metadata.Methods {
		if m == "S256" {
			s256 = true
		}
	}
	if !s256 {
		return nil, errors.New("OIDC provider must advertise S256 PKCE")
	}
	for _, raw := range []string{p.Endpoint().AuthURL, p.Endpoint().TokenURL, metadata.JWKS} {
		u, err := configuredURL(raw, c.Environment == "development")
		if err != nil || u.Scheme != issuer.Scheme || u.Host != issuer.Host {
			return nil, errors.New("OIDC endpoints must share the configured issuer origin")
		}
	}
	return &Provider{issuer: c.IssuerURL, client: client,
		oauth:    oauth2.Config{ClientID: c.ClientID, ClientSecret: c.ClientSecret, Endpoint: p.Endpoint(), RedirectURL: c.RedirectURL, Scopes: []string{oidc.ScopeOpenID, "profile", "email"}},
		verifier: p.Verifier(&oidc.Config{ClientID: c.ClientID, SupportedSigningAlgs: []string{oidc.RS256}})}, nil
}

func (p *Provider) Issuer() string { return p.issuer }

func validVerifier(v string) bool {
	if len(v) < 43 || len(v) > 128 {
		return false
	}
	for _, c := range v {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~", c)) {
			return false
		}
	}
	return true
}

// Caller must store a browser-bound expiring login transaction and consume it
// atomically after checking returned state. This adapter does not establish a
// browser session and must not be called directly from an unguarded callback.
func (p *Provider) AuthorizationURL(state, nonce, verifier string) (string, error) {
	if len(state) < 32 || len(state) > 512 || len(nonce) < 32 || len(nonce) > 512 || !validVerifier(verifier) {
		return "", errors.New("strong state, nonce and PKCE verifier required")
	}
	return p.oauth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), nil
}

type Identity struct {
	Issuer        string
	Subject       string
	Name          string
	Email         string
	EmailVerified bool
}

func (p *Provider) Exchange(ctx context.Context, code, verifier, expectedNonce string) (Identity, error) {
	if code == "" || len(code) > 8192 || !validVerifier(verifier) || len(expectedNonce) < 32 || len(expectedNonce) > 512 {
		return Identity{}, errors.New("invalid OIDC exchange inputs")
	}
	token, err := p.oauth.Exchange(oidc.ClientContext(ctx, p.client), code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Identity{}, errors.New("OIDC code exchange failed")
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || len(raw) > 65536 {
		return Identity{}, errors.New("OIDC ID token missing or too large")
	}
	id, err := p.verifier.Verify(oidc.ClientContext(ctx, p.client), raw)
	if err != nil {
		return Identity{}, errors.New("OIDC ID token verification failed")
	}
	if subtle.ConstantTimeCompare([]byte(id.Nonce), []byte(expectedNonce)) != 1 || strings.TrimSpace(id.Subject) == "" || id.Subject != strings.TrimSpace(id.Subject) {
		return Identity{}, errors.New("OIDC nonce or subject invalid")
	}
	var claims struct {
		AuthorizedParty string `json:"azp"`
		Name            string `json:"name"`
		Email           string `json:"email"`
		EmailVerified   bool   `json:"email_verified"`
	}
	if id.Claims(&claims) != nil || (!id.IssuedAt.IsZero() && id.IssuedAt.After(time.Now().Add(30*time.Second))) || id.IssuedAt.IsZero() {
		return Identity{}, errors.New("OIDC identity claims invalid")
	}
	if (len(id.Audience) > 1 && claims.AuthorizedParty == "") || (claims.AuthorizedParty != "" && claims.AuthorizedParty != p.oauth.ClientID) {
		return Identity{}, errors.New("OIDC authorized party invalid")
	}
	if id.AccessTokenHash != "" && id.VerifyAccessToken(token.AccessToken) != nil {
		return Identity{}, errors.New("OIDC access token binding invalid")
	}
	return Identity{Issuer: id.Issuer, Subject: id.Subject, Name: claims.Name, Email: claims.Email, EmailVerified: claims.EmailVerified}, nil
}
