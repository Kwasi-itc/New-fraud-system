package display

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestValidateStrict(t *testing.T) {
	f, l, target := uuid.New(), uuid.New(), uuid.New()
	version := int64(7)
	s := Settings{Version: version, Fields: []Field{{ID: f, Name: "name", Type: "string"}}, Links: []Link{{ID: l, FieldID: f, Caption: "caption", TargetFields: []Field{{ID: target, Name: "caption", Type: "string"}}}}}
	t.Run("clone_normalize_and_repeat_direct_columns", func(t *testing.T) {
		in := SaveRequest{Version: &version, Caption: "name", Display: map[string]FieldDisplay{f.String(): {Label: " name ", LinkID: &l}}, DisplayColumns: []DisplayColumn{{ID: "a", Type: "direct", FieldID: &f}, {ID: "b", Type: "direct", FieldID: &f}}}
		out, err := ValidateStrict(s, in)
		if err != nil || len(out.Displayed) != 1 || len(out.Order) != 1 || out.Display[f.String()].Label != "name" {
			t.Fatalf("%+v %v", out, err)
		}
		*out.DisplayColumns[0].FieldID = uuid.New()
		choice := out.Display[f.String()]
		*choice.LinkID = uuid.New()
		if *in.DisplayColumns[0].FieldID != f || *in.Display[f.String()].LinkID != l || in.Display[f.String()].Label != " name " {
			t.Fatal("input was mutated")
		}
	})
	t.Run("reject_ambiguous_column_shape", func(t *testing.T) {
		for _, col := range []DisplayColumn{{ID: "x", Type: "direct", FieldID: &f, LinkID: &l}, {ID: "x", Type: "relation", LinkID: &l, TargetFieldID: &target, FieldID: &f}} {
			if _, err := ValidateStrict(s, SaveRequest{Version: &version, DisplayColumns: []DisplayColumn{col}}); err == nil {
				t.Fatal("ambiguous shape accepted")
			}
		}
	})
	t.Run("reject_invalid_text", func(t *testing.T) {
		for _, label := range []string{"bad\x00", string([]byte{255})} {
			if _, err := ValidateStrict(s, SaveRequest{Version: &version, Display: map[string]FieldDisplay{f.String(): {Label: label}}}); err == nil {
				t.Fatal("invalid text accepted")
			}
		}
	})
	t.Run("require_exact_target_and_active_caption", func(t *testing.T) {
		wrong := uuid.New()
		if _, err := ValidateStrict(s, SaveRequest{Version: &version, DisplayColumns: []DisplayColumn{{ID: "x", Type: "relation", LinkID: &l, TargetFieldID: &wrong}}}); err == nil {
			t.Fatal("caption fallback accepted wrong UUID")
		}
		broken := s
		broken.Links = append([]Link{}, s.Links...)
		broken.Links[0].TargetFields = nil
		if _, err := ValidateStrict(broken, SaveRequest{Version: &version, Display: map[string]FieldDisplay{f.String(): {LinkID: &l}}}); err == nil {
			t.Fatal("missing caption accepted")
		}
	})
	t.Run("required_version_and_conflict", func(t *testing.T) {
		if _, err := ValidateStrict(s, SaveRequest{}); err == nil {
			t.Fatal("missing version accepted")
		}
		old := version - 1
		if _, err := ValidateStrict(s, SaveRequest{Version: &old}); !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	})
}
