package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/payload"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

func preparationModel() ports.TenantModel {
	return ports.TenantModel{RevisionID: "rev-1", RecordLookupField: "object_id", Tables: map[string]ports.TenantModelTable{
		"transactions": {Name: "transactions", Fields: map[string]ports.TenantModelField{"amount": {Type: "float"}, "note": {Type: "string", Nullable: true}}},
	}}
}

func TestPreparationRejectsIncompleteWithoutDecisionEffects(t *testing.T) {
	svc := newFailureTestDecisionService(nil, nilScoringConfigRepository(nil), nilScoringRequestRepository(nil), nilScreeningConfigRepository(nil))
	_, err := svc.EvaluateScenario(context.Background(), "tenant-1", "scenario-1", DecisionEvaluationRequest{ObjectID: "t", ObjectType: "transactions", Fields: map[string]any{"object_id": "t"}})
	var failure *payload.Error
	if !errors.As(err, &failure) || failure.Issues[0].Code != "missing_required" {
		t.Fatalf("error %v", err)
	}
	// The nil mutation dependencies would panic if evaluation reached writes.
}

func TestAllScenariosValidatesBeforeEmptyScenarioSelection(t *testing.T) {
	svc := DecisionService{dataModelReader: dataModelReaderStub{model: preparationModel()}, payloadValidator: payload.NewValidator()}
	_, err := svc.EvaluateAllLiveScenarios(context.Background(), "tenant", DecisionEvaluationRequest{ObjectID: "t", ObjectType: "unknown", Fields: map[string]any{"amount": 1}})
	var failure *payload.Error
	if !errors.As(err, &failure) {
		t.Fatalf("error %v", err)
	}
}

func TestPreparedObjectSharedAndSerializedWithRevision(t *testing.T) {
	reader := &countingDataModelReader{model: preparationModel()}
	svc := DecisionService{dataModelReader: reader, payloadValidator: payload.NewValidator()}
	req, err := svc.prepare(context.Background(), "tenant", DecisionEvaluationRequest{ObjectID: " t ", ObjectType: "transactions", Fields: map[string]any{"amount": "42"}})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		next, err := svc.prepare(context.Background(), "tenant", req)
		if err != nil || next.prepared != req.prepared {
			t.Fatalf("preparation repeated: %v", err)
		}
	}
	if reader.count != 1 {
		t.Fatalf("model loads %d", reader.count)
	}
	if _, err := svc.prepare(context.Background(), "another", req); err == nil {
		t.Fatal("prepared tenant boundary ignored")
	}
	body, err := req.JSONBody()
	if err != nil {
		t.Fatal(err)
	}
	var output map[string]any
	if err := json.Unmarshal(body, &output); err != nil {
		t.Fatal(err)
	}
	if output["model_revision"] != "rev-1" || output["object_id"] != "t" {
		t.Fatalf("%s", body)
	}
}

func TestStoredObjectUsesFullValidation(t *testing.T) {
	reader := stubTenantDataReader{records: []ports.TenantRecord{{ObjectID: "t", ObjectType: "transactions", Fields: map[string]any{"object_id": "t", "amount": json.Number("4")}}}}
	req, err := prepareObject(context.Background(), "tenant", DecisionEvaluationRequest{ObjectID: "t", ObjectType: "transactions"}, preparationModel(), reader, payload.NewValidator())
	if err != nil || req.Fields["amount"] != float64(4) {
		t.Fatalf("%#v %v", req, err)
	}
}
