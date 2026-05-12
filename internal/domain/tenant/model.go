package tenant

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusPending Status = "pending"
	StatusActive  Status = "active"
)

type Tenant struct {
	ID          uuid.UUID
	ExternalKey *string
	Name        string
	SchemaName  string
	Status      Status
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type CreateInput struct {
	Name        string
	ExternalKey *string
}

func New(id uuid.UUID, now time.Time, name string, externalKey *string) Tenant {
	return Tenant{
		ID:          id,
		ExternalKey: normalizeOptional(externalKey),
		Name:        strings.TrimSpace(name),
		SchemaName:  SchemaNameFor(id),
		Status:      StatusPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func SchemaNameFor(id uuid.UUID) string {
	return "tenant_" + strings.ReplaceAll(id.String(), "-", "")
}

func normalizeOptional(value *string) *string {
	if value == nil {
		return nil
	}

	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}

	return &trimmed
}
