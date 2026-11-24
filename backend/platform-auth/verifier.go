package auth

import (
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const ClockLeeway = 5 * time.Second
const UserTTL = time.Minute
const ClientTTL = 15 * time.Minute
const MaxClientTTL = time.Hour
const ServiceTTL = time.Minute
const PlatformAudience = "urn:fraud:platform-api"

// ClientAudience represents the entire fraud system, independently of its APIs.
const ClientAudience = "urn:fraud:system"
const PlatformIssuer = "urn:fraud:identity"

type TrustedKey struct {
	ID             string          `json:"kid"`
	PublicKey      string          `json:"public_key_pem"`
	Issuer         string          `json:"issuer"`
	PrincipalTypes []PrincipalType `json:"principal_types"`
	ServiceID      string          `json:"service_id,omitempty"`
	Capabilities   []string        `json:"capabilities,omitempty"`
	Audiences      []string        `json:"audiences"`
}
type KeyRing struct {
	Keys []TrustedKey `json:"keys"`
}
type trustedRSA struct {
	TrustedKey
	key *rsa.PublicKey
}
type Verifier struct {
	keys      map[string]trustedRSA
	audiences []string
	now       func() time.Time
}

// ValidatePlatformSigner checks the issuing process against the same public
// trust distributed to receivers. Configuration mistakes must fail startup,
// rather than producing access tokens every receiver will reject.
func (v *Verifier) ValidatePlatformSigner(key *rsa.PrivateKey, id, issuer string, audiences ...string) error {
	trusted, ok := v.keys[id]
	if !ok || key == nil || trusted.ServiceID != "" || trusted.Issuer != issuer ||
		!slices.Contains(trusted.PrincipalTypes, PrincipalUser) || !slices.Contains(trusted.PrincipalTypes, PrincipalClient) ||
		trusted.key.N.Cmp(key.N) != 0 || trusted.key.E != key.E || len(audiences) == 0 {
		return errors.New("platform signing key does not match its trusted issuer and principal types")
	}
	for _, audience := range audiences {
		if audience == "" || !slices.Contains(trusted.Audiences, audience) {
			return errors.New("platform signing audience is not trusted")
		}
	}
	return nil
}

func NewTrustedVerifier(ring KeyRing, audiences ...string) (*Verifier, error) {
	if len(ring.Keys) == 0 || len(audiences) == 0 {
		return nil, errors.New("trusted keys and receiving audiences are required")
	}
	v := &Verifier{keys: map[string]trustedRSA{}, audiences: slices.Clone(audiences), now: time.Now}
	for _, aud := range audiences {
		if strings.TrimSpace(aud) == "" {
			return nil, errors.New("empty audience")
		}
	}
	for _, cfg := range ring.Keys {
		if cfg.ID == "" || cfg.Issuer == "" || len(cfg.PrincipalTypes) == 0 || len(cfg.Audiences) == 0 {
			return nil, errors.New("incomplete signing key policy")
		}
		if _, ok := v.keys[cfg.ID]; ok {
			return nil, errors.New("duplicate key ID")
		}
		key, err := jwt.ParseRSAPublicKeyFromPEM([]byte(cfg.PublicKey))
		if err != nil || key.N.BitLen() < 2048 {
			return nil, errors.New("RSA public key must be at least 2048 bits")
		}
		service := slices.Contains(cfg.PrincipalTypes, PrincipalService)
		if service && (len(cfg.PrincipalTypes) != 1 || cfg.ServiceID == "" || cfg.Issuer != "urn:fraud:service:"+cfg.ServiceID) {
			return nil, errors.New("service key must bind one service issuer and principal type")
		}
		if service {
			if err = ValidateCapabilities(cfg.Capabilities, PrincipalService, false); err != nil {
				return nil, err
			}
		} else if cfg.ServiceID != "" || len(cfg.Capabilities) > 0 {
			return nil, errors.New("non-service key has service authority")
		}
		for _, t := range cfg.PrincipalTypes {
			if !validPrincipalType(t) {
				return nil, errors.New("unknown key principal type")
			}
		}
		v.keys[cfg.ID] = trustedRSA{cfg, key}
	}
	return v, nil
}
func LoadVerifier(path string, audiences ...string) (*Verifier, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ring KeyRing
	if err = json.Unmarshal(data, &ring); err != nil {
		return nil, err
	}
	return NewTrustedVerifier(ring, audiences...)
}
func FromEnvironment() (*Verifier, error) {
	path := os.Getenv("PLATFORM_AUTH_KEYS_FILE")
	audience := os.Getenv("PLATFORM_AUTH_AUDIENCE")
	if path == "" || audience == "" {
		return nil, errors.New("PLATFORM_AUTH_KEYS_FILE and PLATFORM_AUTH_AUDIENCE are required")
	}
	return LoadVerifier(path, PlatformAudience, ClientAudience, audience)
}
func (v *Verifier) Verify(raw string) (Principal, error) {
	invalid := errors.New("invalid or expired access token")
	if v == nil || len(raw) == 0 || len(raw) > 32768 {
		return Principal{}, invalid
	}
	claims := &Claims{}
	var trusted trustedRSA
	tok, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		key, ok := v.keys[kid]
		if !ok {
			return nil, invalid
		}
		typ, _ := t.Header["typ"].(string)
		if typ != "at+jwt" {
			return nil, invalid
		}
		trusted = key
		return key.key, nil
	}, jwt.WithValidMethods([]string{"RS256"}), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithLeeway(ClockLeeway), jwt.WithTimeFunc(v.now))
	if err != nil || !tok.Valid || claims.IssuedAt == nil || claims.ExpiresAt == nil || claims.ID == "" || claims.Subject == "" || claims.Subject != strings.TrimSpace(claims.Subject) || claims.Issuer != trusted.Issuer || !slices.Contains(trusted.PrincipalTypes, claims.PrincipalType) {
		return Principal{}, invalid
	}
	if len(claims.Audience) != 1 || !slices.Contains(v.audiences, claims.Audience[0]) || !slices.Contains(trusted.Audiences, claims.Audience[0]) {
		return Principal{}, invalid
	}
	p := Principal{Type: claims.PrincipalType, Subject: claims.Subject, TenantID: claims.TenantID, SessionID: claims.SessionID, AuthzVersion: claims.AuthzVersion, Roles: slices.Clone(claims.Roles), Capabilities: map[string]bool{}, OrigType: claims.OrigType, OrigID: claims.OrigID}
	ttl := UserTTL
	names := claims.Capabilities
	switch p.Type {
	case PrincipalUser:
		if claims.Audience[0] == ClientAudience || len(p.Roles) != 1 || strings.TrimSpace(p.Roles[0]) == "" {
			return Principal{}, invalid
		}
		uid, e := uuid.Parse(p.Subject)
		if e != nil || uid == uuid.Nil || p.TenantID == uuid.Nil || p.SessionID == uuid.Nil || p.AuthzVersion < 1 || len(claims.Scopes) > 0 || claims.ServiceID != "" || claims.ClientID != "" || claims.OrigType != "" || claims.OrigID != "" {
			return Principal{}, invalid
		}
		if (slices.Contains(names, "roles:manage") || slices.Contains(names, "roles:assign")) && !slices.Contains(p.Roles, "tenant_admin") {
			return Principal{}, invalid
		}
	case PrincipalClient:
		if claims.Audience[0] != ClientAudience {
			return Principal{}, invalid
		}
		uid, e := uuid.Parse(p.Subject)
		if e != nil || uid == uuid.Nil {
			return Principal{}, invalid
		}
		ttl = MaxClientTTL
		names = claims.Scopes
		if claims.ClientID != p.Subject || p.TenantID == uuid.Nil || p.SessionID != uuid.Nil || p.AuthzVersion != 0 || len(p.Roles) > 0 || len(claims.Capabilities) > 0 || claims.ServiceID != "" || claims.OrigType != "" || claims.OrigID != "" {
			return Principal{}, invalid
		}
	case PrincipalService:
		if claims.Audience[0] == ClientAudience {
			return Principal{}, invalid
		}
		ttl = ServiceTTL
		if claims.ServiceID != trusted.ServiceID || p.Subject != trusted.ServiceID || p.SessionID != uuid.Nil || p.AuthzVersion != 0 || len(p.Roles) > 0 || len(claims.Scopes) > 0 || claims.ClientID != "" || !validPrincipalType(p.OrigType) || p.OrigID == "" {
			return Principal{}, invalid
		}
		for _, cap := range names {
			if !slices.Contains(trusted.Capabilities, cap) {
				return Principal{}, invalid
			}
		}
		if p.TenantID == uuid.Nil {
			for _, cap := range names {
				if !IsControlPlaneCapability(cap) {
					return Principal{}, invalid
				}
			}
		}
	default:
		return Principal{}, invalid
	}
	if claims.ExpiresAt.Time.Sub(claims.IssuedAt.Time) > ttl || !claims.ExpiresAt.After(claims.IssuedAt.Time) || claims.IssuedAt.After(v.now().Add(ClockLeeway)) {
		return Principal{}, invalid
	}
	if err = ValidateCapabilities(names, p.Type, false); err != nil {
		return Principal{}, invalid
	}
	for _, cap := range names {
		p.Capabilities[cap] = true
	}
	p.Admin = p.Can("cases:access-all")
	return p, nil
}

// VerifyPlatformSession is a strict human-only adapter for domain components.
func (v *Verifier) VerifyPlatformSession(raw string) (Principal, error) {
	p, err := v.Verify(raw)
	if err == nil && !p.IsHuman() {
		err = fmt.Errorf("human session required")
	}
	return p, err
}
