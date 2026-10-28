package mysql

import (
	"errors"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/google/uuid"
)

func TestPhysicalNullabilityPlanning(t *testing.T) {
	tenant, table, fields := plannerFixture()
	p, err := NewPhysicalPlanner("shared_app", tenant, table, fields)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range fields {
		t.Run(string(field.Field.DataType), func(t *testing.T) {
			allow, err := p.SetFieldNullable(field.Field.ID, true)
			if err != nil || allow.requireEmpty {
				t.Fatalf("relaxation: %v", err)
			}
			require, err := p.SetFieldNullable(field.Field.ID, false)
			if err != nil || !require.requireEmpty {
				t.Fatalf("tightening precondition: %v", err)
			}
			if allow.step.ID == require.step.ID || strings.Contains(allow.step.SQL, "NOT NULL") || strings.Contains(allow.step.SQL, "DEFAULT") {
				t.Fatal("nullability identity or implicit default")
			}
			if strings.Count(allow.step.SQL, "MODIFY COLUMN") != len(field.Components) || strings.Count(require.step.SQL, " NOT NULL") != len(field.Components) {
				t.Fatal("incomplete component mutation")
			}
			for _, c := range field.Components {
				if !strings.Contains(allow.step.SQL, "`"+c.Name+"`") {
					t.Fatal("mapped column missing")
				}
			}
		})
	}
	if _, err := p.SetFieldNullable(uuid.New(), true); !errors.Is(err, ErrPhysicalConflict) {
		t.Fatalf("foreign field: %v", err)
	}
	for _, field := range fields {
		field.Field.IsEnum = true
		p, err := NewPhysicalPlanner("shared_app", tenant, table, []MappedPhysicalField{field})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.SetFieldNullable(field.Field.ID, true); !errors.Is(err, ErrPhysicalPlanningUnsupported) {
			t.Fatalf("enum accepted: %v", err)
		}
		if field.Field.DataType == datamodel.DataTypeString {
			field.Field.IsEnum, field.Field.IsUnique = false, true
			p, err = NewPhysicalPlanner("shared_app", tenant, table, []MappedPhysicalField{field})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := p.SetFieldNullable(field.Field.ID, true); !errors.Is(err, ErrPhysicalPlanningUnsupported) {
				t.Fatalf("text exact-key protocol bypassed: %v", err)
			}
		}
	}
}
