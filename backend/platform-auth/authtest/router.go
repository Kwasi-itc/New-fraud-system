package authtest

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type domainVerifier struct{}

func (domainVerifier) Verify(raw string) (auth.Principal, error) {
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return auth.Principal{}, err
	}
	var p auth.Principal
	if json.Unmarshal(data, &p) != nil || p.Subject == "" {
		return p, errors.New("invalid domain fixture")
	}
	return p, nil
}

// DomainRouter isolates handler/resource tests from cryptography. It explicitly
// supplies a trusted test principal for each request. Never use it in token,
// missing-credential, signature, or issuer boundary tests; use a real verifier.
func DomainRouter(engine *gin.Engine) *auth.Router {
	engine.Use(func(c *gin.Context) {
		tenant := uuid.New()
		parts := strings.Split(c.Request.URL.Path, "/")
		for i, v := range parts {
			if v == "tenants" && i+1 < len(parts) {
				if id, err := uuid.Parse(parts[i+1]); err == nil {
					tenant = id
				}
			}
		}
		p := auth.Principal{Type: auth.PrincipalUser, Subject: uuid.NewString(), TenantID: tenant, SessionID: uuid.New(), AuthzVersion: 1, Capabilities: map[string]bool{}}
		for _, cap := range auth.Catalog() {
			if cap.Human {
				p.Capabilities[cap.Name] = true
			}
		}
		raw, err := json.Marshal(p)
		if err != nil {
			panic(err)
		}
		c.Request.Header.Set("Authorization", "Bearer "+base64.RawURLEncoding.EncodeToString(raw))
		c.Next()
	})
	return auth.NewRouter(engine, domainVerifier{})
}
