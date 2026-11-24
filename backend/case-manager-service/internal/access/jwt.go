package access

import (
	"errors"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
)

type Claims = identity.Claims
type Verifier struct{ inner *identity.Verifier }

func NewVerifier(keys map[string][]byte, issuer, audience string) (*Verifier, error) {
	v, err := identity.NewVerifier(keys, issuer, audience)
	if err != nil {
		return nil, err
	}
	return &Verifier{v}, nil
}
func (v *Verifier) Verify(raw string) (Principal, error) {
	if v == nil {
		return Principal{}, errors.New("invalid token")
	}
	p, err := v.inner.Verify(raw)
	if err != nil {
		return Principal{}, err
	}
	return FromPlatform(p), nil
}

func NewPlatformVerifier(v *identity.Verifier) *Verifier { return &Verifier{inner: v} }
func (v *Verifier) Platform() *identity.Verifier         { return v.inner }
