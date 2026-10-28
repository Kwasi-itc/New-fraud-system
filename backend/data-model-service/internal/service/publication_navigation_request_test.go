package service

import (
	"crypto/sha256"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

func TestPublicationNavigationRequest(t *testing.T) {
	scope := requestTestScope()
	input := CreateNavigationOptionInput{
		TenantID: scope.TenantID, SourceTableID: scope.TableID,
		SourceFieldID:   uuid.MustParse("44444444-4444-4444-4444-444444444444"),
		TargetTableID:   uuid.MustParse("66666666-6666-6666-6666-666666666666"),
		FilterFieldID:   uuid.MustParse("77777777-7777-7777-7777-777777777777"),
		OrderingFieldID: uuid.MustParse("88888888-8888-8888-8888-888888888888"),
	}
	want := `{"format":"dm-publication-request/v1","tenant_id":"11111111-1111-1111-1111-111111111111","table_id":"22222222-2222-2222-2222-222222222222","actor_id":"33333333-3333-3333-3333-333333333333","condition":{"mode":"exact","version":"55555555-5555-5555-5555-555555555555"},"kind":"navigation.create","body":{"tenant_id":"11111111-1111-1111-1111-111111111111","source_table_id":"22222222-2222-2222-2222-222222222222","source_field_id":"44444444-4444-4444-4444-444444444444","target_table_id":"66666666-6666-6666-6666-666666666666","filter_field_id":"77777777-7777-7777-7777-777777777777","ordering_field_id":"88888888-8888-8888-8888-888888888888"}}`
	encoded, err := encodePublicationRequest(scope, input)
	if err != nil || string(encoded) != want {
		t.Fatalf("navigation wire changed: %s %v", encoded, err)
	}
	base, err := PublicationRequestHash(scope, input)
	if err != nil || base != sha256.Sum256([]byte(want)) {
		t.Fatalf("navigation fingerprint: %v", err)
	}
	t.Run("reject_missing_endpoints", func(t *testing.T) {
		for i := 0; i < reflect.TypeOf(input).NumField(); i++ {
			changed := input
			reflect.ValueOf(&changed).Elem().Field(i).Set(reflect.ValueOf(uuid.Nil))
			if hash, err := PublicationRequestHash(scope, changed); err == nil || hash != ([32]byte{}) {
				t.Fatalf("missing %s accepted", reflect.TypeOf(input).Field(i).Name)
			}
		}
	})
	t.Run("reject_wrong_scope", func(t *testing.T) {
		for _, field := range []string{"TenantID", "SourceTableID"} {
			changed := input
			reflect.ValueOf(&changed).Elem().FieldByName(field).Set(reflect.ValueOf(uuid.New()))
			if hash, err := PublicationRequestHash(scope, changed); err == nil || hash != ([32]byte{}) {
				t.Fatalf("wrong %s accepted", field)
			}
		}
	})
	t.Run("preserve_endpoint_roles", func(t *testing.T) {
		changed := input
		changed.FilterFieldID, changed.OrderingFieldID = changed.OrderingFieldID, changed.FilterFieldID
		hash, err := PublicationRequestHash(scope, changed)
		if err != nil || hash == base {
			t.Fatalf("endpoint roles lost: %v", err)
		}
	})
	t.Run("self_table_is_supported", func(t *testing.T) {
		changed := input
		changed.TargetTableID = changed.SourceTableID
		changed.FilterFieldID = changed.SourceFieldID
		hash, err := PublicationRequestHash(scope, changed)
		if err != nil || hash == base {
			t.Fatalf("self navigation: %v", err)
		}
	})
	if _, err := PublicationRequestHash(scope, &input); err == nil {
		t.Fatal("input pointer accepted")
	}
}
