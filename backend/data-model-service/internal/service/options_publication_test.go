package service

import (
	"crypto/sha256"
	"testing"

	"github.com/google/uuid"
)

func TestPublicationOptionsRequest(t *testing.T) {
	scope := requestTestScope()
	input := ReplaceTableOptionsInput{TableID: scope.TableID, DisplayedFields: []uuid.UUID{}, FieldOrder: nil}
	want := `{"format":"dm-publication-request/v1","tenant_id":"11111111-1111-1111-1111-111111111111","table_id":"22222222-2222-2222-2222-222222222222","actor_id":"33333333-3333-3333-3333-333333333333","condition":{"mode":"exact","version":"55555555-5555-5555-5555-555555555555"},"kind":"table_options.replace","body":{"table_id":"22222222-2222-2222-2222-222222222222","displayed_fields":[],"field_order":null}}`
	raw, err := encodePublicationRequest(scope, input)
	if err != nil || string(raw) != want {
		t.Fatal("options encoding changed", string(raw), err)
	}
	base, err := PublicationRequestHash(scope, input)
	if err != nil || base != sha256.Sum256([]byte(want)) {
		t.Fatal("options hash", err)
	}
	for _, changed := range []ReplaceTableOptionsInput{
		{TableID: scope.TableID},
		{TableID: scope.TableID, DisplayedFields: []uuid.UUID{}, FieldOrder: []uuid.UUID{}},
		{TableID: scope.TableID, DisplayedFields: []uuid.UUID{uuid.New()}},
	} {
		hash, err := PublicationRequestHash(scope, changed)
		if err != nil || hash == base {
			t.Fatal("input distinction lost", err)
		}
	}
	a, b := uuid.New(), uuid.New()
	for _, displayed := range []bool{true, false} {
		first, second := input, input
		if displayed {
			first.DisplayedFields = []uuid.UUID{a, b}
			second.DisplayedFields = []uuid.UUID{b, a}
		} else {
			first.FieldOrder = []uuid.UUID{a, b}
			second.FieldOrder = []uuid.UUID{b, a}
		}
		x, err := PublicationRequestHash(scope, first)
		if err != nil {
			t.Fatal(err)
		}
		y, err := PublicationRequestHash(scope, second)
		if err != nil || x == y {
			t.Fatal("list order lost", err)
		}
	}
	input.TableID = uuid.New()
	if _, err := PublicationRequestHash(scope, input); err == nil {
		t.Fatal("foreign table accepted")
	}
}
