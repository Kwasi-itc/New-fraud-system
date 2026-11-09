package display

import "github.com/google/uuid"

// Projection describes logical values independently of the SQL dialect.
type Projection struct {
	Column
	Field  Field
	Link   *Link
	Target *Field
	Enum   map[string]string
}

func ValidateObjects(objects []Object) error { return validateObjects(objects) }

func ResolveProjection(s Settings) ([]Projection, error) {
	fields := map[uuid.UUID]Field{}
	links := map[uuid.UUID]Link{}
	for _, f := range s.Fields {
		fields[f.ID] = f
	}
	for _, l := range s.Links {
		links[l.ID] = l
	}
	columns := s.DisplayColumns
	legacy := len(columns) == 0
	if legacy {
		for _, f := range orderedFields(s) {
			id := f.ID
			choice := s.Display[id.String()]
			c := DisplayColumn{ID: id.String(), Type: "direct", Label: choice.Label, FieldID: &id}
			if choice.LinkID != nil {
				c.Type = "relation"
				c.LinkID = choice.LinkID
			}
			columns = append(columns, c)
		}
	}
	if len(columns) > MaxColumns {
		return nil, invalid("at most 12 displayed columns are allowed")
	}
	result := []Projection{}
	for _, c := range columns {
		p := Projection{Column: Column{ID: c.ID, Label: c.Label, Category: c.Type}}
		if c.Type == "direct" && c.FieldID != nil {
			f, ok := fields[*c.FieldID]
			if !ok {
				return nil, ErrConflict
			}
			p.Field = f
			p.Enum = f.Enum
			p.Type = f.Type
			if p.Label == "" {
				p.Label = f.Alias
				if p.Label == "" {
					p.Label = f.Name
				}
			}
		} else if c.Type == "relation" && c.LinkID != nil {
			l, ok := links[*c.LinkID]
			if !ok {
				return nil, ErrConflict
			}
			f, ok := fields[l.FieldID]
			if !ok {
				return nil, ErrConflict
			}
			p.Field = f
			p.Link = &l
			for _, target := range l.TargetFields {
				if (c.TargetFieldID != nil && target.ID == *c.TargetFieldID) || (c.TargetFieldID == nil && target.Name == l.Caption) {
					v := target
					p.Target = &v
					break
				}
			}
			if p.Target == nil {
				return nil, ErrConflict
			}
			p.Type = p.Target.Type
			p.Enum = p.Target.Enum
			if legacy {
				p.Type = f.Type
				p.Enum = nil
				if p.Label == "" {
					p.Label = f.Alias
					if p.Label == "" {
						p.Label = f.Name
					}
				}
			} else if p.Label == "" {
				label := p.Target.Alias
				if label == "" {
					label = p.Target.Name
				}
				p.Label = l.Table + "." + label
			}
		} else {
			return nil, invalid("invalid display column")
		}
		result = append(result, p)
	}
	return result, nil
}
