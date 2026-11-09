package mysql

import (
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/google/uuid"
	"strings"
)

// PreviewFieldMapping uses exactly the registry's creation-time naming rule.
// It is for planning before registration; it does not establish readiness.
func PreviewFieldMapping(field datamodel.Field) (MappedPhysicalField, error) {
	if field.ID == uuid.Nil || field.TableID == uuid.Nil || field.TenantID == uuid.Nil {
		return MappedPhysicalField{}, errors.New("field scope required")
	}
	components := []string{"value"}
	if field.DataType == datamodel.DataTypeIPAddress {
		components = []string{"address", "family", "prefix"}
	}
	result := MappedPhysicalField{Field: field}
	for _, component := range components {
		result.Components = append(result.Components, PhysicalField{FieldID: field.ID, Component: component, Name: "field_" + physicalLabel(field.Name, 12, "field") + "_" + strings.ReplaceAll(field.ID.String(), "-", "") + "_" + component})
	}
	return result, nil
}

// physicalLabel is a creation-time display hint, not an identity. The generated
// physical name also contains the full immutable UUID. Limit is an ASCII byte
// budget; non-ASCII/punctuation runs become separators, never executable SQL.
func physicalLabel(value string, limit int, fallback string) string {
	var result strings.Builder
	separator := false
	for _, c := range value {
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			if separator && result.Len() > 0 && result.Len() < limit {
				result.WriteByte('_')
			}
			if result.Len() == limit {
				break
			}
			result.WriteByte(byte(c))
			separator = false
		} else {
			separator = true
		}
		if result.Len() == limit {
			break
		}
	}
	label := strings.TrimRight(result.String(), "_")
	if label == "" {
		return fallback
	}
	return label
}
