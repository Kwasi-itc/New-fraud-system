package access

import (
	"errors"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
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
	return Principal{Kind: User, Subject: p.Subject, TenantID: p.TenantID, Admin: p.Admin, SessionID: p.SessionID, AuthzVersion: p.AuthzVersion}, nil
}
