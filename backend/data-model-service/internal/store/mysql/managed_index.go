package mysql

import (
	"fmt"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/google/uuid"
)

// ManagedIndexPart describes a nonunique lookup key. A text prefix is an
// accelerator only: SQL still compares/orders complete values. It never supplies
// uniqueness or replaces the exact-key dictionaries used by owner constraints.
type ManagedIndexPart struct {
	Column string
	Prefix int
}

// ManagedIndexParts bounds utf8mb4 prefixes and fixed-width components to the
// InnoDB DYNAMIC 3072-byte/16-part budget. Long text remains stored in full.
func (p *PhysicalPlanner) ManagedIndexParts(ids []uuid.UUID) ([]ManagedIndexPart, error) {
	fields, err := p.selectFields(ids)
	if err != nil {
		return nil, err
	}
	var parts []ManagedIndexPart
	texts, fixed := 0, 0
	for _, f := range fields {
		components := []string{"value"}
		if f.field.DataType == datamodel.DataTypeIPAddress {
			components = []string{"family", "address", "prefix"}
		}
		for _, component := range components {
			column := strings.Trim(f.columns[component], "`")
			part := ManagedIndexPart{Column: column}
			if f.field.DataType == datamodel.DataTypeString {
				part.Prefix = 1
				texts++
			} else {
				fixed += 16
			}
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 || len(parts) > 16 {
		return nil, ErrPhysicalPlanningUnsupported
	}
	if texts > 0 {
		prefix := min(191, (3072-fixed)/(4*texts))
		if prefix < 1 {
			return nil, ErrPhysicalPlanningUnsupported
		}
		for i := range parts {
			if parts[i].Prefix != 0 {
				parts[i].Prefix = prefix
			}
		}
	}
	return parts, nil
}

func (p *PhysicalPlanner) CreateManagedIndex(index uuid.UUID, ids []uuid.UUID) (PhysicalStatement, error) {
	if index == uuid.Nil {
		return PhysicalStatement{}, ErrPhysicalConflict
	}
	parts, err := p.ManagedIndexParts(ids)
	if err != nil {
		return PhysicalStatement{}, err
	}
	columns := make([]string, len(parts))
	for i, part := range parts {
		columns[i] = "`" + part.Column + "`"
		if part.Prefix != 0 {
			columns[i] += fmt.Sprintf("(%d)", part.Prefix)
		}
	}
	return p.statement("create_managed_index", "ALTER TABLE "+p.qualified+" ADD INDEX `record_index_"+physicalUUID(index)+"` ("+strings.Join(columns, ",")+")").withNative(PhysicalNativeChange{Kind: "create_managed_index", IndexID: index, FieldIDs: append([]uuid.UUID(nil), ids...)}), nil
}
