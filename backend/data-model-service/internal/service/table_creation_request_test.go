package service

import (
	"crypto/sha256"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestTableCreationRequestHash(t *testing.T) {
	actor := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	input := CreateTableInput{TenantID: uuid.MustParse("22222222-2222-2222-2222-222222222222"), Name: " Records ", Description: "original", Alias: "A", SemanticType: "Customer"}
	hash, err := TableCreationRequestHash(actor, input)
	want := sha256.Sum256([]byte(`{"format":"dm-table-creation/v1","actor_id":"11111111-1111-1111-1111-111111111111","tenant_id":"22222222-2222-2222-2222-222222222222","name":" Records ","description":"original","alias":"A","semantic_type":"Customer"}`))
	if err != nil || hash != want {
		t.Fatal("canonical creation format changed", hash, err)
	}
	for _, change := range []func(*CreateTableInput){func(v *CreateTableInput) { v.Name = "records" }, func(v *CreateTableInput) { v.Description += " " }, func(v *CreateTableInput) { v.Alias = "a" }, func(v *CreateTableInput) { v.SemanticType = "customer" }, func(v *CreateTableInput) { v.TenantID = uuid.New() }} {
		next := input
		change(&next)
		got, err := TableCreationRequestHash(actor, next)
		if err != nil || got == hash {
			t.Fatal("input distinction lost", err)
		}
	}
	if other, err := TableCreationRequestHash(uuid.New(), input); err != nil || other == hash {
		t.Fatal("actor distinction lost", err)
	}
	for _, bad := range []CreateTableInput{{Name: "records"}, {TenantID: input.TenantID, Name: string([]byte{0xff})}, {TenantID: input.TenantID, Name: "records", Description: strings.Repeat("x", (1<<20)+1)}} {
		if hash, err := TableCreationRequestHash(actor, bad); err == nil || hash != ([32]byte{}) {
			t.Fatal("invalid request accepted", err)
		}
	}
}
