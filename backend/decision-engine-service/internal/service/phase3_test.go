package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/google/uuid"
)

type spyRollupClient struct {
	mu       sync.Mutex
	requests []RollupDefinitionRequest
}

func (s *spyRollupClient) EnsureRollupDefinition(_ context.Context, _ string, req RollupDefinitionRequest) error {
	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.mu.Unlock()
	return nil
}

func TestPublicationServiceStartPreparationRequestsRollupsNonBlockingWithCap(t *testing.T) {
	t.Parallel()

	makeAggRule := func(id, valueField string) scenario.Rule {
		raw, _ := json.Marshal(domainast.Node{
			Function: "gt",
			Children: []domainast.Node{
				{
					Function: "Aggregator",
					NamedChildren: map[string]domainast.Node{
						"tableName":  {Constant: "transactions"},
						"fieldName":  {Constant: valueField},
						"aggregator": {Constant: "SUM"},
						"filters": {
							Function: "List",
							Children: []domainast.Node{
								{
									Function: "filter",
									NamedChildren: map[string]domainast.Node{
										"fieldName": {Constant: "merchant_id"},
										"operator":  {Constant: "eq"},
										"value":     {Constant: "m1"},
									},
								},
								{
									Function: "filter",
									NamedChildren: map[string]domainast.Node{
										"fieldName": {Constant: "created_at"},
										"operator":  {Constant: "gte"},
										"value":     {Constant: "2026-11-01T00:00:00Z"},
									},
								},
							},
						},
					},
				},
				{Constant: 100.0},
			},
		})
		return scenario.Rule{
			ID:          id,
			TenantID:    "t1",
			IterationID: "i1",
			Name:        "rule-" + id,
			Formula:     raw,
		}
	}

	trueFormula, _ := json.Marshal(domainast.Node{Constant: true})
	review, block, decline := 10, 20, 30
	dmReader := &publicationDataModelReader{
		model: ports.TenantModel{
			RevisionID:        "rev-1",
			RecordLookupField: "object_id",
			Tables: map[string]ports.TenantModelTable{
				"transactions": {
					ID:   "tbl-1",
					Name: "transactions",
					Fields: map[string]ports.TenantModelField{
						"object_id":   {Name: "object_id", Type: "string"},
						"merchant_id": {Name: "merchant_id", Type: "string"},
						"created_at":  {Name: "created_at", Type: "timestamptz"},
						"amount1":     {Name: "amount1", Type: "float"},
						"amount2":     {Name: "amount2", Type: "float"},
						"amount3":     {Name: "amount3", Type: "float"},
						"amount4":     {Name: "amount4", Type: "float"},
						"amount5":     {Name: "amount5", Type: "float"},
					},
				},
			},
		},
		jobs: []ports.ManagedIndexJob{
			{
				ID:             "idx-1",
				TableName:      "transactions",
				IndexType:      "search",
				Status:         "applied",
				Columns:        []string{"merchant_id", "created_at"},
				IncludeColumns: []string{"amount1", "amount2", "amount3", "amount4", "amount5"},
			},
		},
	}

	pubRepo := nilPublicationRepository(nil)
	scnRepo := multiScenarioRepoStub{scenarios: []scenario.Scenario{{ID: "s1", TenantID: "t1", TriggerObjectType: "transactions"}}}
	iterRepo := multiIterationRepoStub{iterations: map[string]scenario.Iteration{
		"i1": {
			ID:                           "i1",
			ScenarioID:                   "s1",
			TenantID:                     "t1",
			Status:                       scenario.IterationStatusCommitted,
			TriggerFormula:               trueFormula,
			ScoreReviewThreshold:         &review,
			ScoreBlockAndReviewThreshold: &block,
			ScoreDeclineThreshold:        &decline,
		},
	}}
	ruleRepo := ruleListMapStub{byIter: map[string][]scenario.Rule{
		"i1": {
			makeAggRule("r1", "amount1"),
			makeAggRule("r2", "amount2"),
			makeAggRule("r3", "amount3"),
			makeAggRule("r4", "amount4"),
			makeAggRule("r5", "amount5"),
		},
	}}

	svc := NewPublicationService(
		rollbackTxManagerStub{},
		fixedIDGenerator{id: uuid.MustParse("11111111-1111-1111-1111-111111111111")},
		fixedClock{now: time.Date(2026, 11, 25, 21, 0, 0, 0, time.UTC)},
		pubRepo,
		scnRepo,
		iterRepo,
		ruleRepo,
		dmReader,
	)
	rollupClient := &spyRollupClient{}
	svc.SetRollupClient(rollupClient, 4)

	status, err := svc.StartPreparation(context.Background(), "t1", "s1", "i1")
	if err != nil {
		t.Fatalf("StartPreparation() error = %v", err)
	}
	if !status.PreparationFinished || status.PreparationRequired {
		t.Fatalf("expected index preparation finished (rollups must not block Publish), got %+v", status)
	}
	if len(rollupClient.requests) != 4 {
		t.Fatalf("requested %d rollup definitions, want cap of 4", len(rollupClient.requests))
	}
	if len(status.Warnings) != 1 {
		t.Fatalf("expected 1 cap-exceeded warning, got %v", status.Warnings)
	}
}
