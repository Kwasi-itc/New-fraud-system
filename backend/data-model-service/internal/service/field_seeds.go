package service

import (
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
)

// ValidateFieldSeeds runs before preparing physical work. As in PostgreSQL,
// enum membership is validated by ingestion against the published catalog;
// the physical column retains its base type.
func ValidateFieldSeeds(input CreateFieldInput) error {
	if len(input.EnumValues) > 1000 {
		return fmt.Errorf("at most 1000 enum seeds are supported")
	}
	values, labels := map[string]bool{}, map[string]bool{}
	for _, seed := range input.EnumValues {
		if seed.SortOrder < -2147483648 || seed.SortOrder > 2147483647 {
			return fmt.Errorf("enum sort order must fit a signed 32-bit integer")
		}
		if err := datamodel.ValidateEnumValueCreate(datamodel.Field{IsEnum: input.IsEnum, DataType: input.DataType}, seed.Value, seed.Label); err != nil {
			return err
		}
		if values[seed.Value] || labels[seed.Label] {
			return fmt.Errorf("enum values and labels must be distinct")
		}
		values[seed.Value], labels[seed.Label] = true, true
	}
	return nil
}
