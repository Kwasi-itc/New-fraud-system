package service

import (
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/google/uuid"
)

func TestPreparedFieldCreationRequestHash(t *testing.T) {
	scope := PublicationRequestContext{TenantID: uuid.New(), TableID: uuid.New(), ActorID: uuid.New(), Condition: contract.PublicationCondition{Mode: contract.PublicationExactVersion, Version: uuid.New()}}
	field := uuid.New()
	input := CreateFieldInput{TableID: scope.TableID, Name: "score", DataType: datamodel.DataTypeInt, Nullable: true}
	first, err := PreparedFieldCreationRequestHash(scope, field, input)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := PreparedFieldCreationRequestHash(scope, field, input)
	if err != nil || replay != first {
		t.Fatal("unstable request", err)
	}
	other, err := PreparedFieldCreationRequestHash(scope, uuid.New(), input)
	if err != nil || other == first {
		t.Fatal("field identity not bound", err)
	}
	if _, err := PreparedFieldCreationRequestHash(scope, uuid.Nil, input); err == nil {
		t.Fatal("empty allocated identity")
	}
	changed := input
	changed.Alias = "score label"
	other, err = PreparedFieldCreationRequestHash(scope, field, changed)
	if err != nil || other == first {
		t.Fatal("original input not bound", err)
	}
	newScope := scope
	newScope.ActorID = uuid.New()
	other, err = PreparedFieldCreationRequestHash(newScope, field, input)
	if err != nil || other == first {
		t.Fatal("actor not bound", err)
	}
	newScope = scope
	newScope.Condition.Version = uuid.New()
	other, err = PreparedFieldCreationRequestHash(newScope, field, input)
	if err != nil || other == first {
		t.Fatal("previous version not bound", err)
	}
}
