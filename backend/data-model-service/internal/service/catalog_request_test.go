package service

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/google/uuid"
	"testing"
)

func TestCatalogRequestIdentity(t *testing.T) {
	scope := PublicationRequestContext{TenantID: uuid.New(), TableID: uuid.New(), ActorID: uuid.New(), Condition: contract.PublicationCondition{Mode: contract.PublicationExactVersion, Version: uuid.New()}}
	id := uuid.New()
	label := "Label"
	commands := []any{DeleteFieldInput{FieldID: id}, DeleteEnumValueInput{EnumValueID: id}, UpdateFieldEnumValueInput{EnumValueID: id, Label: &label}}
	seen := map[[32]byte]bool{}
	for _, command := range commands {
		h, e := PublicationRequestHash(scope, command)
		if e != nil || seen[h] {
			t.Fatal("missing or colliding command hash", e)
		}
		seen[h] = true
		again, e := PublicationRequestHash(scope, command)
		if e != nil || again != h {
			t.Fatal("unstable hash", e)
		}
	}
	for _, command := range []any{DeleteFieldInput{}, DeleteEnumValueInput{}} {
		if _, e := PublicationRequestHash(scope, command); e == nil {
			t.Fatal("missing identity accepted")
		}
	}
}
