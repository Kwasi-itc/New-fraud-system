package workers

import (
	"errors"
	"time"
)

// LegacyExport is an immutable source observation, not a target INSERT model.
// ErrorCount must come from error history, never from claim attempts. Preserve
// payload bytes and original timestamps until the owner approves translation.
type LegacyExport struct {
	Kind                   string
	PayloadVersion         int
	Payload                []byte
	State                  string
	Attempt, MaxAttempts   int
	ErrorCount             *int64
	CreatedAt, AvailableAt time.Time
	FinalizedAt            *time.Time
}

// ExportScope must be obtained through the owner's authoritative resource lookup
// in the export/reconciliation snapshot. It is not accepted from a caller's
// untrusted tenant claim. Global jobs explicitly have neither identity.
type ExportScope struct{ TenantID, ResourceID string }

type CheckedExport struct {
	Identity PayloadIdentity
	Source   LegacyExport
}

var ErrDrainRequired = errors.New("running legacy work must be drained and reconciled before export")
var ErrExportContract = errors.New("legacy work export requires explicit scope, state and valid history")

// CheckLegacyExport provides common fail-closed validation for all catalog kinds.
// It does NOT authorize access, resolve a missing tenant, map owner states, create
// target schema, insert jobs or prove external effects were delivered once.
// Owners must revalidate scope/receipts and apply their explicit state mapping
// together with the immutable import receipt before activating a target delivery.
func CheckLegacyExport(source LegacyExport, scope ExportScope, databaseNow time.Time) (CheckedExport, error) {
	identity, err := DecodeIdentity(source.Kind, source.PayloadVersion, source.Payload)
	if err != nil {
		return CheckedExport{}, err
	}
	if identity.Global {
		if scope.TenantID != "" || scope.ResourceID != "" {
			return CheckedExport{}, ErrExportContract
		}
	} else {
		if !canonicalUUID(scope.TenantID) || scope.ResourceID != identity.ResourceID || (identity.TenantID != "" && identity.TenantID != scope.TenantID) {
			return CheckedExport{}, ErrExportContract
		}
		identity.TenantID = scope.TenantID
	}
	if source.State == "running" {
		return CheckedExport{}, ErrDrainRequired
	}
	terminal := false
	switch source.State {
	case "available", "pending", "retryable", "scheduled":
	case "completed", "cancelled", "discarded":
		terminal = true
	default:
		return CheckedExport{}, ErrExportContract
	}
	if source.MaxAttempts < 1 || source.MaxAttempts > 2147483647 {
		return CheckedExport{}, ErrExportContract
	}
	if err := ValidateErrorHistory(source.Attempt, source.ErrorCount, false); err != nil {
		return CheckedExport{}, err
	}
	if !scheduleTime(databaseNow) || !scheduleTime(source.CreatedAt) || !scheduleTime(source.AvailableAt) || source.CreatedAt.After(databaseNow) || terminal != (source.FinalizedAt != nil) {
		return CheckedExport{}, ErrExportContract
	}
	if source.FinalizedAt != nil && (!scheduleTime(*source.FinalizedAt) || source.FinalizedAt.Before(source.CreatedAt) || source.FinalizedAt.After(databaseNow)) {
		return CheckedExport{}, ErrExportContract
	}
	// The checked result must not alias a mutable source row/driver buffer.
	source.Payload = append([]byte(nil), source.Payload...)
	count := *source.ErrorCount
	source.ErrorCount = &count
	if source.FinalizedAt != nil {
		finished := source.FinalizedAt.UTC()
		source.FinalizedAt = &finished
	}
	source.CreatedAt = source.CreatedAt.UTC()
	source.AvailableAt = source.AvailableAt.UTC()
	return CheckedExport{Identity: identity, Source: source}, nil
}
