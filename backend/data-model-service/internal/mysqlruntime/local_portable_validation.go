package mysqlruntime

import (
	"fmt"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
)

// Validate the entire graph before starting any physical creation. Owner services
// still revalidate relationships under locks when they are committed.
func validateLocalPortable(d service.PortableDataModelDocument) error {
	if err := service.ValidatePortableDocument(d); err != nil {
		return err
	}
	count := len(d.Tables) + len(d.Links) + len(d.Pivots)
	fields := map[string]map[string]service.PortableField{}
	norm := datamodel.NormalizeName
	for _, t := range d.Tables {
		if err := datamodel.ValidateTableCreate(t.Name); err != nil {
			return err
		}
		if err := datamodel.ValidateSemanticType(t.SemanticType); err != nil {
			return err
		}
		fs := map[string]service.PortableField{"object_id": {DataType: "string", IsUnique: true}, "updated_at": {DataType: "timestamp"}}
		for _, f := range t.Fields {
			typ, err := datamodel.ParseDataType(f.DataType)
			if err != nil {
				return err
			}
			if err = datamodel.ValidateFieldCreate(f.Name, typ, f.IsEnum, f.IsUnique); err != nil {
				return err
			}
			if err = service.ValidateFieldSeeds(service.CreateFieldInput{Name: f.Name, DataType: typ, IsEnum: f.IsEnum, IsUnique: f.IsUnique, EnumValues: f.EnumValues}); err != nil {
				return err
			}
			fs[norm(f.Name)] = f
			count += 1 + len(f.EnumValues)
		}
		if c := norm(t.CaptionField); c != "" {
			if f, ok := fs[c]; !ok || f.DataType != "string" {
				return fmt.Errorf("caption must reference a string field on %s", t.Name)
			}
		}
		if t.Options != nil {
			if len(t.Options.DisplayedFields) > 12 {
				return fmt.Errorf("at most 12 displayed fields are allowed")
			}
			for _, names := range [][]string{t.Options.DisplayedFields, t.Options.FieldOrder} {
				seen := map[string]bool{}
				for _, name := range names {
					if seen[norm(name)] {
						return fmt.Errorf("duplicate option field")
					}
					seen[norm(name)] = true
				}
			}
		}
		fields[norm(t.Name)] = fs
		count += len(t.NavigationOptions)
	}
	if count > 200 {
		return fmt.Errorf("portable import is limited to 200 tables, fields, enum values and relationships combined")
	}
	links := map[string]service.PortableLink{}
	for _, l := range d.Links {
		if err := datamodel.ValidateLinkName(l.Name); err != nil {
			return err
		}
		p, c := fields[norm(l.ParentTable)][norm(l.ParentField)], fields[norm(l.ChildTable)][norm(l.ChildField)]
		if p.DataType != "string" || c.DataType != "string" || !p.IsUnique {
			return fmt.Errorf("link requires string fields and a unique parent")
		}
		links[norm(l.Name)] = l
	}
	for _, p := range d.Pivots {
		if (strings.TrimSpace(p.Field) == "") == (len(p.PathLinks) == 0) {
			return fmt.Errorf("pivot requires exactly one field or path")
		}
		if p.Field != "" && fields[norm(p.BaseTable)][norm(p.Field)].DataType != "string" {
			return fmt.Errorf("pivot field must be string")
		}
		current := norm(p.BaseTable)
		for _, name := range p.PathLinks {
			l := links[norm(name)]
			if norm(l.ChildTable) != current {
				return fmt.Errorf("pivot path must be connected")
			}
			current = norm(l.ParentTable)
		}
	}
	for _, t := range d.Tables {
		for _, o := range t.NavigationOptions {
			if fields[norm(t.Name)][norm(o.SourceField)].DataType != "string" || norm(o.FilterField) == norm(o.OrderingField) {
				return fmt.Errorf("invalid navigation source or ordering")
			}
			valid := false
			for _, l := range d.Links {
				if norm(l.ParentTable) == norm(t.Name) && norm(l.ParentField) == norm(o.SourceField) && norm(l.ChildTable) == norm(o.TargetTable) && norm(l.ChildField) == norm(o.FilterField) {
					valid = true
				}
			}
			if norm(t.Name) == norm(o.TargetTable) {
				if norm(o.SourceField) != norm(o.FilterField) {
					return fmt.Errorf("self navigation fields must match")
				}
				for _, p := range d.Pivots {
					if norm(p.BaseTable) == norm(t.Name) && norm(p.Field) == norm(o.SourceField) {
						valid = true
					}
				}
			}
			if !valid {
				return fmt.Errorf("navigation must have a reverse link or self pivot")
			}
		}
	}
	return nil
}
