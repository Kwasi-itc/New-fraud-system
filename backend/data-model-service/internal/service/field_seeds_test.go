package service

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"testing"
)

func TestFieldSeedValidation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input CreateFieldInput
		valid bool
	}{
		{"sort order overflow", CreateFieldInput{IsEnum: true, DataType: datamodel.DataTypeString, EnumValues: []CreateFieldEnumValueSeed{{Value: "x", Label: "X", SortOrder: 2147483648}}}, false},
		{"string", CreateFieldInput{IsEnum: true, DataType: datamodel.DataTypeString, EnumValues: []CreateFieldEnumValueSeed{{Value: "open", Label: "Open"}}}, true},
		{"integer", CreateFieldInput{IsEnum: true, DataType: datamodel.DataTypeInt, EnumValues: []CreateFieldEnumValueSeed{{Value: "1", Label: "One"}}}, true},
		{"invalid integer", CreateFieldInput{IsEnum: true, DataType: datamodel.DataTypeInt, EnumValues: []CreateFieldEnumValueSeed{{Value: "wrong", Label: "Wrong"}}}, false},
		{"non enum", CreateFieldInput{DataType: datamodel.DataTypeString, EnumValues: []CreateFieldEnumValueSeed{{Value: "open", Label: "Open"}}}, false},
		{"duplicate value", CreateFieldInput{IsEnum: true, DataType: datamodel.DataTypeString, EnumValues: []CreateFieldEnumValueSeed{{Value: "open", Label: "Open"}, {Value: "open", Label: "Other"}}}, false},
		{"duplicate label", CreateFieldInput{IsEnum: true, DataType: datamodel.DataTypeString, EnumValues: []CreateFieldEnumValueSeed{{Value: "open", Label: "Open"}, {Value: "closed", Label: "Open"}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateFieldSeeds(tc.input); (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}
