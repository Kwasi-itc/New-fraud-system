// Package display owns presentation configuration and bounded current-record reads.
package display

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

const MaxColumns = 12
const MaxRecords = 100

var ErrConflict = errors.New("display settings changed; reload before saving")

type Invalid struct{ Message string }

func (e *Invalid) Error() string   { return e.Message }
func invalid(message string) error { return &Invalid{message} }

type Field struct {
	ID    uuid.UUID         `json:"id"`
	Name  string            `json:"name"`
	Alias string            `json:"alias"`
	Type  string            `json:"data_type"`
	Enum  map[string]string `json:"enum_labels"`
}

type Link struct {
	ID           uuid.UUID `json:"id"`
	FieldID      uuid.UUID `json:"field_id"`
	Name         string    `json:"name"`
	Table        string    `json:"target_table"`
	Field        string    `json:"target_field"`
	Caption      string    `json:"caption_field"`
	TargetFields []Field   `json:"target_fields,omitempty"`
}

type FieldDisplay struct {
	Label  string     `json:"label,omitempty"`
	LinkID *uuid.UUID `json:"link_id,omitempty"`
}

type DisplayColumn struct {
	ID            string     `json:"id"`
	Type          string     `json:"type"` // "direct" or "relation"
	Label         string     `json:"label"`
	FieldID       *uuid.UUID `json:"field_id,omitempty"`
	LinkID        *uuid.UUID `json:"link_id,omitempty"`
	TargetFieldID *uuid.UUID `json:"target_field_id,omitempty"`
}

type Settings struct {
	TableID        uuid.UUID               `json:"table_id"`
	TenantID       uuid.UUID               `json:"-"`
	Name           string                  `json:"name"`
	Schema         string                  `json:"-"`
	Version        int64                   `json:"version"`
	Caption        string                  `json:"caption_field"`
	Displayed      []uuid.UUID             `json:"displayed_fields"`
	Order          []uuid.UUID             `json:"field_order"`
	Display        map[string]FieldDisplay `json:"field_display"`
	DisplayColumns []DisplayColumn         `json:"display_columns"`
	Fields         []Field                 `json:"fields"`
	Links          []Link                  `json:"links"`
}

type SaveRequest struct {
	Version        *int64                  `json:"version"`
	Caption        string                  `json:"caption_field"`
	Displayed      []uuid.UUID             `json:"displayed_fields,omitempty"`
	Order          []uuid.UUID             `json:"field_order,omitempty"`
	Display        map[string]FieldDisplay `json:"field_display,omitempty"`
	DisplayColumns []DisplayColumn         `json:"display_columns,omitempty"`
}

func validate(s Settings, in SaveRequest) error {
	if in.Version == nil {
		return invalid("settings version is required")
	}
	if *in.Version != s.Version {
		return ErrConflict
	}

	fields := map[uuid.UUID]Field{}
	captionOK := in.Caption == ""
	for _, f := range s.Fields {
		fields[f.ID] = f
		if f.Name == in.Caption && f.Type == "string" {
			captionOK = true
		}
	}
	if !captionOK {
		return invalid("caption must be an active string field on this table")
	}

	links := map[uuid.UUID]Link{}
	for _, l := range s.Links {
		links[l.ID] = l
	}

	if len(in.DisplayColumns) > 0 {
		if len(in.DisplayColumns) > MaxColumns {
			return invalid("at most 12 displayed columns are allowed")
		}
		seenIDs := map[string]bool{}
		for _, col := range in.DisplayColumns {
			if strings.TrimSpace(col.ID) == "" {
				return invalid("column identifier is required")
			}
			if seenIDs[col.ID] {
				return invalid("column identifiers must be distinct")
			}
			seenIDs[col.ID] = true
			if utf8.RuneCountInString(col.Label) > 100 {
				return invalid("column labels must be at most 100 characters")
			}
			switch col.Type {
			case "direct":
				if col.FieldID == nil {
					return invalid("direct display column requires a field identifier")
				}
				if _, ok := fields[*col.FieldID]; !ok {
					return invalid("display field does not belong to this table")
				}
			case "relation":
				if col.LinkID == nil || col.TargetFieldID == nil {
					return invalid("relation display column requires a relationship and target field")
				}
				link, ok := links[*col.LinkID]
				if !ok {
					return invalid("relationship not found on this table")
				}
				targetOK := false
				for _, tf := range link.TargetFields {
					if tf.ID == *col.TargetFieldID {
						targetOK = true
						break
					}
				}
				if !targetOK && link.Caption != "" {
					// Also permit if it matches the parent caption field name
					targetOK = true
				}
				if !targetOK {
					return invalid("target field does not belong to related table")
				}
			default:
				return invalid("unknown display column type")
			}
		}
		return nil
	}

	if len(in.Displayed) > MaxColumns {
		return invalid("at most 12 displayed columns are allowed")
	}
	for _, ids := range [][]uuid.UUID{in.Displayed, in.Order} {
		seen := map[uuid.UUID]bool{}
		for _, id := range ids {
			if _, ok := fields[id]; !ok || seen[id] {
				return invalid("columns must be distinct active fields on this table")
			}
			seen[id] = true
		}
	}
	for key, choice := range in.Display {
		id, err := uuid.Parse(key)
		if err != nil || id.String() != key {
			return invalid("invalid display field identifier")
		}
		if _, ok := fields[id]; !ok {
			return invalid("display field does not belong to this table")
		}
		if utf8.RuneCountInString(choice.Label) > 100 {
			return invalid("column labels must be at most 100 characters")
		}
		choice.Label = strings.TrimSpace(choice.Label)
		in.Display[key] = choice
		if choice.LinkID != nil {
			link, ok := links[*choice.LinkID]
			if !ok || link.FieldID != id || link.Caption == "" {
				return invalid("linked captions require an active, unique same-tenant relationship with a caption")
			}
		}
	}
	return nil
}

type Object struct {
	Type string `json:"object_type"`
	ID   string `json:"object_id"`
}
type Column struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Type     string `json:"data_type"`
	Category string `json:"category,omitempty"`
}
type Record struct {
	Object
	Caption string         `json:"caption"`
	Found   bool           `json:"found"`
	Values  map[string]any `json:"values"`
}
type Batch struct {
	Columns map[string][]Column `json:"columns"`
	Records []Record            `json:"records"`
}

func validateObjects(objects []Object) error {
	if len(objects) == 0 || len(objects) > MaxRecords {
		return invalid("provide 1 to 100 records")
	}
	for _, o := range objects {
		if o.Type == "" || len(o.Type) > 63 || o.ID == "" || len(o.ID) > 1024 {
			return invalid(fmt.Sprintf("invalid source record %q", o.Type))
		}
	}
	return nil
}
