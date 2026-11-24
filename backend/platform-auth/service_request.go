package auth

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/google/uuid"
)

type Origin struct {
	Type     PrincipalType `json:"type,omitempty"`
	ID       string        `json:"id,omitempty"`
	TenantID uuid.UUID     `json:"tenant_id,omitempty"`
}
type originKey struct{}

func CaptureOrigin(ctx context.Context) Origin {
	if p, ok := FromContext(ctx); ok {
		if p.IsService() && p.OrigID != "" {
			return Origin{p.OrigType, p.OrigID, p.TenantID}
		}
		return Origin{p.Type, p.Subject, p.TenantID}
	}
	o, _ := ctx.Value(originKey{}).(Origin)
	return o
}

// WithOrigin restores persisted provenance. It never installs an authenticated
// principal and therefore cannot grant human authority to a worker.
func WithOrigin(ctx context.Context, o Origin) context.Context {
	return context.WithValue(ctx, originKey{}, o)
}

var environmentSigner struct {
	once   sync.Once
	signer *Signer
	err    error
}

func SignerFromEnvironment() (*Signer, error) {
	environmentSigner.once.Do(func() {
		environmentSigner.signer, environmentSigner.err = LoadSigner(os.Getenv("PLATFORM_AUTH_SIGNING_KEY_FILE"), os.Getenv("PLATFORM_AUTH_SIGNING_KEY_ID"), os.Getenv("PLATFORM_AUTH_SERVICE_ID"))
	})
	return environmentSigner.signer, environmentSigner.err
}

// ValidateEnvironment verifies both receiving trust and the process's signing
// identity before an API/worker starts accepting work.
func ValidateEnvironment() error {
	v, err := FromEnvironment()
	if err != nil {
		return err
	}
	s, err := LoadSigner(os.Getenv("PLATFORM_AUTH_SIGNING_KEY_FILE"), os.Getenv("PLATFORM_AUTH_SIGNING_KEY_ID"), os.Getenv("PLATFORM_AUTH_SERVICE_ID"))
	if err != nil {
		return err
	}
	key, ok := v.keys[s.KeyID]
	if !ok || key.ServiceID != s.ServiceID || key.key.N.Cmp(s.Key.N) != 0 || key.key.E != s.Key.E {
		return errors.New("service signing key does not match its trusted identity")
	}
	return nil
}
func RequestTenant(req *http.Request) (uuid.UUID, error) {
	tenant := uuid.Nil
	parts := strings.Split(strings.Trim(req.URL.Path, "/"), "/")
	for i, part := range parts {
		if part == "tenants" && i+1 < len(parts) {
			id, err := uuid.Parse(parts[i+1])
			if err != nil {
				return uuid.Nil, err
			}
			tenant = id
			break
		}
	}
	origin := CaptureOrigin(req.Context())
	if tenant == uuid.Nil {
		tenant = origin.TenantID
	}
	if tenant == uuid.Nil {
		return uuid.Nil, errors.New("tenant context is required for the internal request")
	}
	if origin.TenantID != uuid.Nil && origin.TenantID != tenant {
		return uuid.Nil, errors.New("internal request tenant differs from originator")
	}
	return tenant, nil
}
func AuthorizeServiceRequest(req *http.Request, tenant uuid.UUID, audience string, capabilities []string, signer *Signer) error {
	if tenant == uuid.Nil {
		return errors.New("internal request requires a tenant")
	}
	origin := CaptureOrigin(req.Context())
	if origin.TenantID != uuid.Nil && origin.TenantID != tenant {
		return errors.New("internal request tenant mismatch")
	}
	if origin.ID != "" && (!validPrincipalType(origin.Type) || origin.TenantID != tenant) {
		return errors.New("invalid persisted origin")
	}
	var err error
	if signer == nil {
		signer, err = SignerFromEnvironment()
		if err != nil {
			return err
		}
	}
	p := Principal{}
	if origin.ID != "" {
		p = Principal{Type: origin.Type, Subject: origin.ID, TenantID: tenant}
	}
	token, err := signer.Sign(tenant, audience, capabilities, p)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}
