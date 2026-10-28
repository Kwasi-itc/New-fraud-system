package contract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/google/uuid"
)

// Publication is the persisted ingestion contract, not a publication command.
type Publication struct {
	TenantID uuid.UUID
	TableID  uuid.UUID
	Version  uuid.UUID
	Payload  json.RawMessage
}

// PrepareIngestion selects a payload and opaque version for a complete, locked
// metadata snapshot. It does not read, write, lock or certify DDL readiness.
// The caller must read previous and persist the result in the same publication
// transaction, after readiness checks. Changed=false requires no database write.
// A nil previous denotes first publication, not a failed database read.
func PrepareIngestion(table datamodel.Table, fields []datamodel.Field, values []datamodel.FieldEnumValue, previous *Publication, newVersion func() (uuid.UUID, error)) (next Publication, changed bool, err error) {
	payload, err := BuildIngestion(table, fields, values)
	if err != nil {
		return Publication{}, false, err
	}
	if previous != nil {
		if previous.TenantID != table.TenantID || previous.TableID != table.ID || previous.Version == uuid.Nil {
			return Publication{}, false, errors.New("previous publication identity inconsistent with table")
		}
		equal, err := equalIngestionPayload(previous.Payload, payload)
		if err != nil {
			return Publication{}, false, err
		}
		if equal {
			// Copy canonical payload; never return a mutable alias of stored bytes.
			return Publication{TenantID: table.TenantID, TableID: table.ID, Version: previous.Version, Payload: payload}, false, nil
		}
	}
	if newVersion == nil {
		return Publication{}, false, errors.New("publication version generator required")
	}
	version, err := newVersion()
	if err != nil {
		return Publication{}, false, fmt.Errorf("allocate publication version: %w", err)
	}
	if version == uuid.Nil || (previous != nil && version == previous.Version) {
		return Publication{}, false, errors.New("publication version must be fresh and nonzero")
	}
	return Publication{TenantID: table.TenantID, TableID: table.ID, Version: version, Payload: payload}, true, nil
}

// Ingestion documents contain strings, booleans, objects and arrays, never JSON
// numbers (even numeric enum values are strings). Decode with UseNumber so an
// unexpected numeric stored value cannot be rounded into equality. Array order,
// null vs empty, missing keys, extra keys and JSON types remain significant.
// Object key order, whitespace and equivalent JSON escaping are insignificant.
// This is not a general implementation of PostgreSQL numeric JSONB equality.
func equalIngestionPayload(stored, desired json.RawMessage) (bool, error) {
	if !utf8.Valid(stored) || !json.Valid(stored) {
		return false, errors.New("stored ingestion publication is invalid JSON or UTF-8")
	}
	var left, right any
	decoder := json.NewDecoder(bytes.NewReader(stored))
	decoder.UseNumber()
	if err := decoder.Decode(&left); err != nil {
		return false, fmt.Errorf("decode stored ingestion publication: %w", err)
	}
	if err := json.Unmarshal(desired, &right); err != nil {
		return false, fmt.Errorf("decode desired ingestion publication: %w", err)
	}
	return reflect.DeepEqual(left, right), nil
}
