package app

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	"github.com/google/uuid"
)

func (c Config) APIAuth() (*access.Verifier, []uuid.UUID, error) {
	if c.ServiceAuthMode != "token" || len(c.ServiceAuthToken) < 32 {
		return nil, nil, fmt.Errorf("case-manager API requires SERVICE_AUTH_MODE=token and a service token of at least 32 bytes")
	}
	var tenants []uuid.UUID
	for _, raw := range strings.Split(c.ServiceTenantIDs, ",") {
		id, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil || id == uuid.Nil {
			return nil, nil, fmt.Errorf("SERVICE_AUTH_TENANT_IDS must contain explicit tenant UUIDs")
		}
		tenants = append(tenants, id)
	}
	content, err := os.ReadFile(c.UserJWTKeysFile)
	if err != nil {
		return nil, nil, fmt.Errorf("read USER_JWT_KEYS_FILE: %w", err)
	}
	var rawKeys map[string]string
	if err := json.Unmarshal(content, &rawKeys); err != nil {
		return nil, nil, fmt.Errorf("JWT key file must map key IDs to PEM public keys: %w", err)
	}
	keys := map[string][]byte{}
	for id, pem := range rawKeys {
		keys[id] = []byte(pem)
	}
	verifier, err := access.NewVerifier(keys, c.UserJWTIssuer, c.UserJWTAudience)
	return verifier, tenants, err
}
