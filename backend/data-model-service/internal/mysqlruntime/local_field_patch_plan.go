package mysqlruntime

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
)

func physicalFieldPatchStatements(planner *store.PhysicalPlanner, current datamodel.Field, input service.UpdateFieldInput, definer installation.Definer) ([]store.PhysicalStatement, error) {
	var statements []store.PhysicalStatement
	if current.DataType == datamodel.DataTypeString {
		if input.Nullable != nil && *input.Nullable != current.Nullable {
			step, err := planner.SetTextNullable(current.ID, *input.Nullable)
			if err != nil {
				return nil, err
			}
			statements = append(statements, step)
		}
		if input.IsUnique != nil && *input.IsUnique != current.IsUnique {
			var steps []store.PhysicalStatement
			var err error
			if *input.IsUnique {
				steps, err = planner.EnableTextUnique(current.ID, definer)
			} else {
				steps, err = planner.DisableTextUnique(current.ID, definer)
			}
			if err != nil {
				return nil, err
			}
			statements = append(statements, steps...)
		}
		return statements, nil
	}
	appendStep := func(step store.PhysicalStatement, err error) error {
		if err != nil {
			return err
		}
		statements = append(statements, step)
		return nil
	}
	if input.Nullable != nil && *input.Nullable != current.Nullable {
		if err := appendStep(planner.SetFieldNullableChecked(current.ID, *input.Nullable)); err != nil {
			return nil, err
		}
	}
	if input.IsUnique != nil && *input.IsUnique != current.IsUnique {
		if *input.IsUnique {
			if err := appendStep(planner.CreateFieldUniqueIndex(current.ID)); err != nil {
				return nil, err
			}
		} else {
			if err := appendStep(planner.DropFieldUniqueIndex(current.ID)); err != nil {
				return nil, err
			}
		}
	}
	if len(statements) == 0 {
		if err := appendStep(planner.SetFieldNullableChecked(current.ID, current.Nullable)); err != nil {
			return nil, err
		}
	}
	return statements, nil
}
