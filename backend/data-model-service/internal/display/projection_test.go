package display

import (
	"github.com/google/uuid"
	"testing"
)

func TestPortableProjection(t *testing.T) {
	id, target, link := uuid.New(), uuid.New(), uuid.New()
	s := Settings{Fields: []Field{{ID: id, Name: "code", Alias: "Code", Type: "string", Enum: map[string]string{"a": "A"}}}, Links: []Link{{ID: link, FieldID: id, Table: "parent", Caption: "caption", TargetFields: []Field{{ID: target, Name: "caption", Alias: "Title", Type: "string"}}}}, Displayed: []uuid.UUID{id}, Order: []uuid.UUID{id}, Display: map[string]FieldDisplay{}}
	cols, e := ResolveProjection(s)
	if e != nil || len(cols) != 1 || cols[0].Label != "Code" || cols[0].Enum["a"] != "A" {
		t.Fatal(cols, e)
	}
	s.Display[id.String()] = FieldDisplay{LinkID: &link, Label: "Linked"}
	cols, e = ResolveProjection(s)
	if e != nil || cols[0].Target.ID != target || cols[0].Label != "Linked" || cols[0].Enum != nil {
		t.Fatal(cols, e)
	}
	s.DisplayColumns = []DisplayColumn{{ID: "related", Type: "relation", LinkID: &link, TargetFieldID: &target}}
	cols, e = ResolveProjection(s)
	if e != nil || cols[0].Label != "parent.Title" || cols[0].ID != "related" {
		t.Fatal(cols, e)
	}
	foreign := uuid.New()
	s.DisplayColumns[0].TargetFieldID = &foreign
	if _, e = ResolveProjection(s); e == nil {
		t.Fatal("foreign target must not fall back to a caption")
	}
}
