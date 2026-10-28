package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/display"

	"github.com/google/uuid"
)

const displayResultKind = "display.settings/v1"

// Settings intentionally omits internal scope from its API JSON. Preserve that
// scope explicitly in the versioned receipt instead of changing the API shape.
type retainedDisplayResult struct {
	TenantID uuid.UUID        `json:"tenant_id"`
	Schema   string           `json:"schema"`
	Settings display.Settings `json:"settings"`
}

func (s *DisplayStore) readDisplayResult(ctx context.Context, q ownerResultReader, table, operation uuid.UUID) (display.Settings, error) {
	raw, err := (ownerResults{s.schema, s.tenant, s.maxBytes}).read(ctx, q, table, operation, displayResultKind)
	if err != nil {
		return display.Settings{}, err
	}
	var retained retainedDisplayResult
	if err = json.Unmarshal(raw, &retained); err != nil {
		return display.Settings{}, err
	}
	result := retained.Settings
	result.TenantID, result.Schema = retained.TenantID, retained.Schema
	if result.TenantID != s.tenant || result.TableID != table || result.Version < 0 {
		return display.Settings{}, errors.New("invalid retained display identity")
	}
	return result, nil
}

func (s *DisplayStore) writeDisplayResult(ctx context.Context, tx *sql.Tx, table, operation uuid.UUID, result display.Settings) error {
	if result.TenantID != s.tenant || result.TableID != table {
		return errors.New("display result scope mismatch")
	}
	raw, err := json.Marshal(retainedDisplayResult{TenantID: result.TenantID, Schema: result.Schema, Settings: result})
	if err != nil {
		return err
	}
	return (ownerResults{s.schema, s.tenant, s.maxBytes}).write(ctx, tx, table, operation, displayResultKind, raw)
}
