package auth

import (
	"context"
	"encoding/pem"
	"fmt"
	"slices"
)

func NewVerifier(keys map[string][]byte, issuer, audience string) (*Verifier, error) {
	ring := KeyRing{}
	for id, key := range keys {
		if block, _ := pem.Decode(key); block == nil {
			return nil, fmt.Errorf("invalid PEM public key")
		}
		ring.Keys = append(ring.Keys, TrustedKey{ID: id, PublicKey: string(key), Issuer: issuer, PrincipalTypes: []PrincipalType{PrincipalUser}, Audiences: []string{audience}})
	}
	return NewTrustedVerifier(ring, audience)
}

type requiredCapabilityKey struct{}

func RequiredCapability(ctx context.Context) string {
	value, _ := ctx.Value(requiredCapabilityKey{}).(string)
	return value
}

// CheckIdentity is a domain adapter for existing transaction boundaries.
// The declared route capability is revalidated; no fixed role list is consulted.
func CheckIdentity(ctx context.Context, q Queryer, p Principal, lock bool) error {
	capability := RequiredCapability(ctx)
	if capability == "" {
		capability = "cases:read"
	}
	if err := CheckCapability(ctx, q, p, capability, lock); err != nil {
		return err
	}
	if p.Admin && capability != "cases:access-all" {
		return CheckCapability(ctx, q, p, "cases:access-all", lock)
	}
	return nil
}

type additionalAuthorityKey struct{}

// WithCurrentCapability adds a known, operation-specific requirement. It is set
// by trusted owner code (for example a review that also changes a whitelist),
// never decoded from a request's claimed permissions.
func WithCurrentCapability(ctx context.Context, capability string) context.Context {
	if _, ok := LookupCapability(capability); !ok {
		panic("unknown current capability")
	}
	previous, _ := ctx.Value(additionalAuthorityKey{}).([]string)
	values := append(append([]string(nil), previous...), capability)
	return context.WithValue(ctx, additionalAuthorityKey{}, values)
}
func CurrentCapabilities(ctx context.Context) []string {
	values, _ := ctx.Value(additionalAuthorityKey{}).([]string)
	values = append([]string(nil), values...)
	if capability := RequiredCapability(ctx); RequiresCurrentAuthority(capability) {
		values = append(values, capability)
	}
	slices.Sort(values)
	return slices.Compact(values)
}
