package display

import (
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

// ValidateStrict validates every persisted representation, including legacy
// options when display_columns is populated. The MySQL owner cannot rely on the
// PostgreSQL validation triggers to reject a wrong target after API validation.
// Settings must contain only active, tenant-consistent eligible relationships.
// The returned request owns its slices/map and is safe to normalize independently.
func ValidateStrict(s Settings, in SaveRequest) (SaveRequest, error) {
	fail := func(message string) (SaveRequest, error) { return SaveRequest{}, invalid(message) }
	if in.Version == nil {
		return fail("settings version is required")
	}
	if *in.Version != s.Version {
		return SaveRequest{}, ErrConflict
	}
	validText := func(v string) bool { return utf8.ValidString(v) && !strings.ContainsRune(v, 0) }
	fields := make(map[uuid.UUID]Field, len(s.Fields))
	captionOK := in.Caption == ""
	for _, f := range s.Fields {
		fields[f.ID] = f
		if f.Name == in.Caption && f.Type == "string" {
			captionOK = true
		}
	}
	if !validText(in.Caption) || !captionOK {
		return fail("caption must be an active string field on this table")
	}
	links := make(map[uuid.UUID]Link, len(s.Links))
	for _, l := range s.Links {
		links[l.ID] = l
	}
	if len(in.DisplayColumns) > MaxColumns || len(in.Displayed) > MaxColumns {
		return fail("at most 12 displayed columns are allowed")
	}
	out := in
	out.Displayed = append([]uuid.UUID{}, in.Displayed...)
	out.Order = append([]uuid.UUID{}, in.Order...)
	out.DisplayColumns = append([]DisplayColumn{}, in.DisplayColumns...)
	out.Display = make(map[string]FieldDisplay, len(in.Display))
	for _, ids := range [][]uuid.UUID{in.Displayed, in.Order} {
		seen := make(map[uuid.UUID]bool, len(ids))
		for _, id := range ids {
			if _, ok := fields[id]; !ok || seen[id] {
				return fail("columns must be distinct active fields on this table")
			}
			seen[id] = true
		}
	}
	for key, choice := range in.Display {
		id, err := uuid.Parse(key)
		if err != nil || id.String() != key {
			return fail("invalid display field identifier")
		}
		if _, ok := fields[id]; !ok {
			return fail("display field does not belong to this table")
		}
		if !validText(choice.Label) || utf8.RuneCountInString(choice.Label) > 100 {
			return fail("invalid column label")
		}
		choice.Label = strings.TrimSpace(choice.Label)
		if choice.LinkID != nil {
			link, ok := links[*choice.LinkID]
			caption := false
			for _, f := range link.TargetFields {
				if f.Name == link.Caption && f.Type == "string" {
					caption = true
				}
			}
			if !ok || link.FieldID != id || !caption {
				return fail("linked captions require an active unique relationship with a caption")
			}
			value := *choice.LinkID
			choice.LinkID = &value
		}
		out.Display[key] = choice
	}
	seen := make(map[string]bool, len(in.DisplayColumns))
	for i, col := range in.DisplayColumns {
		if !validText(col.ID) || strings.TrimSpace(col.ID) == "" || seen[col.ID] {
			return fail("column identifiers must be nonempty and distinct")
		}
		seen[col.ID] = true
		if !validText(col.Label) || utf8.RuneCountInString(col.Label) > 100 {
			return fail("invalid column label")
		}
		switch col.Type {
		case "direct":
			if col.FieldID == nil || col.LinkID != nil || col.TargetFieldID != nil {
				return fail("direct column requires only a field identifier")
			}
			if _, ok := fields[*col.FieldID]; !ok {
				return fail("display field does not belong to this table")
			}
			value := *col.FieldID
			out.DisplayColumns[i].FieldID = &value
		case "relation":
			if col.LinkID == nil || col.TargetFieldID == nil || col.FieldID != nil {
				return fail("relation column requires only a relationship and target field")
			}
			link, ok := links[*col.LinkID]
			if !ok {
				return fail("relationship not found on this table")
			}
			found := false
			for _, f := range link.TargetFields {
				if f.ID == *col.TargetFieldID {
					found = true
					break
				}
			}
			if !found {
				return fail("target field does not belong to related table")
			}
			linkID, fieldID := *col.LinkID, *col.TargetFieldID
			out.DisplayColumns[i].LinkID, out.DisplayColumns[i].TargetFieldID = &linkID, &fieldID
		default:
			return fail("unknown display column type")
		}
	}
	if len(in.DisplayColumns) > 0 && len(in.Displayed) == 0 {
		// Repeated direct columns are legal with distinct column identifiers.
		// Their compatibility field lists are sets, preserving first occurrence.
		displayed, ordered := map[uuid.UUID]bool{}, map[uuid.UUID]bool{}
		for _, id := range out.Order {
			ordered[id] = true
		}
		for _, col := range out.DisplayColumns {
			if col.Type == "direct" {
				id := *col.FieldID
				if !displayed[id] {
					out.Displayed = append(out.Displayed, id)
					displayed[id] = true
				}
				if !ordered[id] {
					out.Order = append(out.Order, id)
					ordered[id] = true
				}
			}
		}
	}
	return out, nil
}
