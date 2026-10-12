package investigator

import (
	"crypto/rsa"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type Claims struct {
	jwt.RegisteredClaims
	TenantID     string   `json:"tenant_id"`
	Roles        []string `json:"roles"`
	SessionID    string   `json:"sid"`
	AuthzVersion int64    `json:"authz_version"`
}

// Verifier trusts only configured public keys. Token-controlled jku/x5u URLs and
// algorithms are never used. Multiple PEM keys support overlapping key rotation.
type Verifier struct {
	keys             map[string]*rsa.PublicKey
	issuer, audience string
}

func NewVerifier(keys map[string][]byte, issuer, audience string) (*Verifier, error) {
	if len(keys) == 0 || strings.TrimSpace(issuer) == "" || strings.TrimSpace(audience) == "" {
		return nil, errors.New("JWT keys, issuer and audience are required")
	}
	v := &Verifier{keys: map[string]*rsa.PublicKey{}, issuer: issuer, audience: audience}
	for id, pem := range keys {
		if id == "" {
			return nil, errors.New("JWT key ID is required")
		}
		key, err := jwt.ParseRSAPublicKeyFromPEM(pem)
		if err != nil {
			return nil, err
		}
		if key.N.BitLen() < 2048 {
			return nil, errors.New("JWT RSA keys must be at least 2048 bits")
		}
		v.keys[id] = key
	}
	return v, nil
}
func (v *Verifier) verifyClaims(raw string) (*Claims, error) {
	if v == nil || len(raw) == 0 || len(raw) > 16384 {
		return nil, errors.New("invalid token")
	}
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		key, ok := v.keys[kid]
		if !ok {
			return nil, errors.New("unknown signing key")
		}
		return key, nil
	}, jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer(v.issuer), jwt.WithAudience(v.audience), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithLeeway(30*time.Second))
	if err != nil || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

// VerifyPlatformSession verifies identity only. The owner must check current
// database membership, permissions and resource access before doing any work.
// Unlike the legacy case verifier, platform sessions always require UUID actors
// and session/version claims, regardless of USER_REQUIRE_SESSION.
func (v *Verifier) VerifyPlatformSession(raw string) (Principal, error) {
	claims, err := v.verifyClaims(raw)
	if err != nil {
		return Principal{}, err
	}
	actor, e1 := uuid.Parse(claims.Subject)
	tenant, e2 := uuid.Parse(claims.TenantID)
	sid, e3 := uuid.Parse(claims.SessionID)
	if e1 != nil || e2 != nil || e3 != nil || actor == uuid.Nil || tenant == uuid.Nil || sid == uuid.Nil || claims.AuthzVersion < 1 || claims.IssuedAt == nil {
		return Principal{}, errors.New("invalid platform session claims")
	}
	return Principal{Subject: actor.String(), TenantID: tenant, SessionID: sid, AuthzVersion: claims.AuthzVersion}, nil
}

func (v *Verifier) Verify(raw string) (Principal, error) {
	claims, err := v.verifyClaims(raw)
	if err != nil {
		return Principal{}, err
	}
	id, err := uuid.Parse(claims.TenantID)
	if err != nil || id == uuid.Nil || strings.TrimSpace(claims.Subject) == "" || claims.Subject != strings.TrimSpace(claims.Subject) || claims.IssuedAt == nil {
		return Principal{}, errors.New("invalid identity claims")
	}
	p := Principal{Subject: claims.Subject, TenantID: id}
	if claims.SessionID != "" {
		p.SessionID, err = uuid.Parse(claims.SessionID)
		if err != nil || p.SessionID == uuid.Nil || claims.AuthzVersion < 1 {
			return Principal{}, errors.New("invalid session claims")
		}
		p.AuthzVersion = claims.AuthzVersion
	} else if os.Getenv("USER_REQUIRE_SESSION") == "true" {
		return Principal{}, errors.New("active platform session required")
	}
	allowed := false
	for _, role := range claims.Roles {
		if role == "case_investigator" {
			allowed = true
		}
		if role == "case_manager_admin" {
			p.Admin = true
			allowed = true
		}
	}
	if !allowed {
		return Principal{}, errors.New("case role is required")
	}
	return p, nil
}
