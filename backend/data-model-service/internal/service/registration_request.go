package service

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/google/uuid"
)

// RegistrationRequestContext comes from trusted platform identity and a stable
// owner-allocated tenant ID. Neither identity may be accepted from unverified
// headers. Hashing does not authorize registration or allocate/recover its ID.
type RegistrationRequestContext struct {
	TenantID uuid.UUID
	ActorID  uuid.UUID
}

type registrationRequestBody struct {
	Name        string  `json:"name"`
	ExternalKey *string `json:"external_key"`
}

// RegistrationRequestHash pins dm-tenant-registration/v1. It binds the original
// request before tenant.New normalizes metadata. Reuse the same scope/input on
// recovery; no session token, generated timestamp or operation key is included.
// The separate operation key must be retained by the future receipt protocol.
func RegistrationRequestHash(scope RegistrationRequestContext, input tenant.CreateInput) ([32]byte, error) {
	encoded, err := encodeRegistrationRequest(scope, input)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}

func encodeRegistrationRequest(scope RegistrationRequestContext, input tenant.CreateInput) ([]byte, error) {
	if scope.TenantID == uuid.Nil || scope.ActorID == uuid.Nil {
		return nil, errors.New("verified actor and stable allocated tenant identity required")
	}
	if !utf8.ValidString(input.Name) || (input.ExternalKey != nil && !utf8.ValidString(*input.ExternalKey)) {
		return nil, errors.New("registration request contains invalid UTF-8")
	}
	return json.Marshal(struct {
		Format   string                  `json:"format"`
		TenantID uuid.UUID               `json:"tenant_id"`
		ActorID  uuid.UUID               `json:"actor_id"`
		Kind     string                  `json:"kind"`
		Body     registrationRequestBody `json:"body"`
	}{"dm-tenant-registration/v1", scope.TenantID, scope.ActorID, "tenant.create", registrationRequestBody{input.Name, input.ExternalKey}})
}
