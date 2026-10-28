package service

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"github.com/google/uuid"
)

// TableCreationRequestHash identifies the original command before allocating a
// table ID. It is separate from the publication hash, which binds that allocated
// table. Keep the operation key across retries; hashing does not authorize.
func TableCreationRequestHash(actor uuid.UUID, input CreateTableInput) ([32]byte, error) {
	if actor == uuid.Nil || input.TenantID == uuid.Nil {
		return [32]byte{}, errors.New("trusted actor and tenant required")
	}
	total := 0
	for _, s := range []string{input.Name, input.Description, input.Alias, input.SemanticType} {
		total += len(s)
		if total > 1<<20 {
			return [32]byte{}, errors.New("creation command exceeds 1 MiB")
		}
		if !utf8.ValidString(s) {
			return [32]byte{}, errors.New("creation request contains invalid UTF-8")
		}
	}
	raw, err := json.Marshal(struct {
		Format       string    `json:"format"`
		Actor        uuid.UUID `json:"actor_id"`
		Tenant       uuid.UUID `json:"tenant_id"`
		Name         string    `json:"name"`
		Description  string    `json:"description"`
		Alias        string    `json:"alias"`
		SemanticType string    `json:"semantic_type"`
	}{"dm-table-creation/v1", actor, input.TenantID, input.Name, input.Description, input.Alias, input.SemanticType})
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(raw), nil
}
