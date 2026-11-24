package auth

import (
	"crypto/rsa"
	"errors"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"os"
	"time"
)

type Signer struct {
	Key              *rsa.PrivateKey
	KeyID, ServiceID string
}

func LoadSigner(path, kid, serviceID string) (*Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	key, err := jwt.ParseRSAPrivateKeyFromPEM(data)
	if err != nil || key.N.BitLen() < 2048 || kid == "" || serviceID == "" {
		return nil, errors.New("invalid service signing configuration")
	}
	return &Signer{Key: key, KeyID: kid, ServiceID: serviceID}, nil
}
func SignAccessToken(key *rsa.PrivateKey, kid string, claims Claims) (string, error) {
	if key == nil || key.N.BitLen() < 2048 || kid == "" {
		return "", errors.New("signing key is required")
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	token.Header["typ"] = "at+jwt"
	return token.SignedString(key)
}
func (s *Signer) Sign(tenant uuid.UUID, audience string, capabilities []string, origin Principal) (string, error) {
	if s == nil || s.ServiceID == "" || audience == "" {
		return "", errors.New("service signer is not configured")
	}
	if err := ValidateCapabilities(capabilities, PrincipalService, false); err != nil {
		return "", err
	}
	if origin.Subject == "" {
		origin = Principal{Type: PrincipalService, Subject: s.ServiceID, TenantID: tenant}
	}
	if !validPrincipalType(origin.Type) || origin.TenantID != tenant {
		return "", errors.New("originator tenant mismatch")
	}
	origType, origID := origin.Type, origin.Subject
	if origin.IsService() && origin.OrigID != "" {
		origType, origID = origin.OrigType, origin.OrigID
	}
	if tenant == uuid.Nil {
		for _, c := range capabilities {
			if !IsControlPlaneCapability(c) {
				return "", errors.New("tenant is required")
			}
		}
	}
	now := time.Now()
	claims := Claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: "urn:fraud:service:" + s.ServiceID, Subject: s.ServiceID, Audience: jwt.ClaimStrings{audience}, ID: uuid.NewString(), IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(ServiceTTL))}, PrincipalType: PrincipalService, TenantID: tenant, ServiceID: s.ServiceID, Capabilities: append([]string(nil), capabilities...), OrigType: origType, OrigID: origID}
	return SignAccessToken(s.Key, s.KeyID, claims)
}
