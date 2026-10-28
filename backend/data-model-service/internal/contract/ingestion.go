// Package contract owns the portable data-model ingestion publication payload.
// Persistence, version allocation and physical-schema readiness belong to the
// publication transaction; constructing a document does not publish it.
package contract

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/google/uuid"
)

type document struct {
	ID       uuid.UUID        `json:"id"`
	Name     string           `json:"name"`
	Archived bool             `json:"archived"`
	Fields   map[string]field `json:"fields"`
}
type field struct {
	ID         uuid.UUID          `json:"id"`
	Name       string             `json:"name"`
	DataType   datamodel.DataType `json:"data_type"`
	Nullable   bool               `json:"nullable"`
	IsEnum     bool               `json:"is_enum"`
	IsUnique   bool               `json:"is_unique"`
	Archived   bool               `json:"archived"`
	EnumValues []enumValue        `json:"enum_values"`
}
type enumValue struct {
	ID    uuid.UUID `json:"id"`
	Value string    `json:"value"`
	Label string    `json:"label"`
}

// BuildIngestion constructs the payload of migration 000005 without SQL or I/O.
// Supply a complete table-scoped snapshot, including archived fields. Display
// metadata is intentionally absent. Enum ordering is sort_order then UUID bytes.
// Inconsistent ownership and duplicate field identities fail rather than being
// silently overwritten in a JSON object. Caller slices are never reordered.
func BuildIngestion(table datamodel.Table, fields []datamodel.Field, values []datamodel.FieldEnumValue) (json.RawMessage, error) {
	if table.ID == uuid.Nil || table.TenantID == uuid.Nil {
		return nil, errors.New("table identity required")
	}
	if !utf8.ValidString(table.Name) {
		return nil, errors.New("table name is not valid UTF-8")
	}
	doc := document{ID: table.ID, Name: table.Name, Archived: table.Archived, Fields: make(map[string]field, len(fields))}
	byID := make(map[uuid.UUID]datamodel.Field, len(fields))
	for _, f := range fields {
		if !utf8.ValidString(f.Name) || !utf8.ValidString(string(f.DataType)) {
			return nil, errors.New("field contract text is not valid UTF-8")
		}
		if f.ID == uuid.Nil || f.TenantID != table.TenantID || f.TableID != table.ID {
			return nil, errors.New("field ownership inconsistent with table")
		}
		if _, ok := byID[f.ID]; ok {
			return nil, errors.New("duplicate field identity")
		}
		if _, ok := doc.Fields[f.Name]; ok {
			return nil, errors.New("duplicate field name")
		}
		byID[f.ID] = f
		doc.Fields[f.Name] = field{ID: f.ID, Name: f.Name, DataType: f.DataType, Nullable: f.Nullable, IsEnum: f.IsEnum, IsUnique: f.IsUnique, Archived: f.Archived, EnumValues: []enumValue{}}
	}
	ordered := append([]datamodel.FieldEnumValue(nil), values...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].SortOrder != ordered[j].SortOrder {
			return ordered[i].SortOrder < ordered[j].SortOrder
		}
		return bytes.Compare(ordered[i].ID[:], ordered[j].ID[:]) < 0
	})
	seen := make(map[uuid.UUID]bool, len(ordered))
	for _, v := range ordered {
		if !utf8.ValidString(v.Value) || !utf8.ValidString(v.Label) {
			return nil, errors.New("enum contract text is not valid UTF-8")
		}
		f, ok := byID[v.FieldID]
		if !ok || v.TenantID != table.TenantID || v.ID == uuid.Nil || seen[v.ID] {
			return nil, errors.New("enum identity or ownership inconsistent with table")
		}
		seen[v.ID] = true
		// PostgreSQL includes no enum values for fields whose is_enum is false.
		if !f.IsEnum {
			continue
		}
		entry := doc.Fields[f.Name]
		entry.EnumValues = append(entry.EnumValues, enumValue{ID: v.ID, Value: v.Value, Label: v.Label})
		doc.Fields[f.Name] = entry
	}
	return json.Marshal(doc)
}
