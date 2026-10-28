package display

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestSaveRequestHash(t *testing.T) {
	tenant, table, actor := uuid.New(), uuid.New(), uuid.New()
	version := int64(0)
	condition := contract.PublicationCondition{Mode: contract.PublicationAnyVersion}
	base := SaveRequest{Version: &version}
	hash := func(in SaveRequest) [32]byte {
		t.Helper()
		h, e := SaveRequestHash(tenant, table, actor, condition, in)
		if e != nil {
			t.Fatal(e)
		}
		return h
	}
	original := hash(base)
	for name, change := range map[string]func(*SaveRequest){
		"caption":         func(r *SaveRequest) { r.Caption = "name" },
		"version":         func(r *SaveRequest) { v := int64(1); r.Version = &v },
		"empty_order":     func(r *SaveRequest) { r.Order = []uuid.UUID{} },
		"empty_displayed": func(r *SaveRequest) { r.Displayed = []uuid.UUID{} },
		"empty_display":   func(r *SaveRequest) { r.Display = map[string]FieldDisplay{} },
		"empty_columns":   func(r *SaveRequest) { r.DisplayColumns = []DisplayColumn{} },
	} {
		t.Run(name, func(t *testing.T) {
			r := base
			change(&r)
			if hash(r) == original {
				t.Fatal("command change did not change hash")
			}
		})
	}
	r := base
	r.Display = map[string]FieldDisplay{"b": {Label: "B"}, "a": {Label: "A"}}
	h := hash(r)
	r.Display = map[string]FieldDisplay{"a": {Label: "A"}, "b": {Label: "B"}}
	if hash(r) != h {
		t.Fatal("map order")
	}
	r.Order = []uuid.UUID{table, actor}
	h = hash(r)
	r.Order = []uuid.UUID{actor, table}
	if hash(r) == h {
		t.Fatal("array order")
	}
	for _, scope := range [][3]uuid.UUID{{uuid.New(), table, actor}, {tenant, uuid.New(), actor}, {tenant, table, uuid.New()}} {
		h, e := SaveRequestHash(scope[0], scope[1], scope[2], condition, base)
		if e != nil || h == original {
			t.Fatal("scope not bound", e)
		}
	}
	r = base
	r.DisplayColumns = []DisplayColumn{{ID: "one", Type: "field", Label: "Name", FieldID: &table, LinkID: &actor, TargetFieldID: &tenant}}
	h = hash(r)
	for _, change := range []func(*DisplayColumn){func(c *DisplayColumn) { c.ID = "two" }, func(c *DisplayColumn) { c.Type = "link" }, func(c *DisplayColumn) { c.Label = "Other" }, func(c *DisplayColumn) { c.FieldID = nil }, func(c *DisplayColumn) { c.LinkID = nil }, func(c *DisplayColumn) { c.TargetFieldID = nil }} {
		v := r
		v.DisplayColumns = append([]DisplayColumn(nil), r.DisplayColumns...)
		change(&v.DisplayColumns[0])
		if hash(v) == h {
			t.Fatal("column input omitted")
		}
	}
	for _, change := range []func(*SaveRequest){func(r *SaveRequest) { r.Caption = "\xff" }, func(r *SaveRequest) { r.Display = map[string]FieldDisplay{"\xff": {}} }, func(r *SaveRequest) { r.Display = map[string]FieldDisplay{"a": {Label: "\xff"}} }, func(r *SaveRequest) { r.Version = nil }} {
		v := base
		change(&v)
		if _, e := SaveRequestHash(tenant, table, actor, condition, v); e == nil {
			t.Fatal("invalid command accepted")
		}
	}
	raw, e := encodeSaveRequest(tenant, table, actor, condition, base)
	if e != nil {
		t.Fatal(e)
	}
	want := `{"format":"dm-display-save/v1","tenant_id":"` + tenant.String() + `","table_id":"` + table.String() + `","actor_id":"` + actor.String() + `","condition":{"mode":"any","version":"00000000-0000-0000-0000-000000000000"},"display_version":0,"caption_field":"","displayed_fields":null,"field_order":null,"field_display":null,"display_columns":null}`
	if strings.TrimSpace(string(raw)) != want {
		t.Fatalf("wire changed: %s", raw)
	}
}
