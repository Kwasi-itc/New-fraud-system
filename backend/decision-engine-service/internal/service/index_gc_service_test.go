package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type gcIterationRepoStub struct {
	byScenario map[string][]scenario.Iteration
}

func (s gcIterationRepoStub) Create(context.Context, scenario.Iteration) (scenario.Iteration, error) {
	return scenario.Iteration{}, nil
}
func (s gcIterationRepoStub) GetByID(context.Context, string, string, string) (scenario.Iteration, error) {
	return scenario.Iteration{}, nil
}
func (s gcIterationRepoStub) ListByScenario(_ context.Context, _, scenarioID string) ([]scenario.Iteration, error) {
	return s.byScenario[scenarioID], nil
}
func (s gcIterationRepoStub) Commit(context.Context, string, string, string, time.Time) (scenario.Iteration, error) {
	return scenario.Iteration{}, nil
}
func (s gcIterationRepoStub) ListLiveScheduled(context.Context, int) ([]scenario.Iteration, error) {
	return nil, nil
}
func (s gcIterationRepoStub) NextVersion(context.Context, string, string) (int, error) {
	return 1, nil
}
func (s gcIterationRepoStub) Update(context.Context, scenario.Iteration) (scenario.Iteration, error) {
	return scenario.Iteration{}, nil
}

type recordingIndexDropper struct {
	dropped []string
}

func (d *recordingIndexDropper) DropManagedIndexJob(_ context.Context, _, jobID string) error {
	d.dropped = append(d.dropped, jobID)
	return nil
}

func TestIndexGCServiceRetainsOldIndexWhileLiveIterationReferencesIt(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 25, 20, 0, 0, 0, time.UTC)
	oldCompleted := now.Add(-48 * time.Hour)
	recentCompleted := now.Add(-2 * time.Hour)

	// Older live scenario still uses COUNT(object_id) on (merchant_id, created_at) without INCLUDE(amount).
	countFormula, _ := json.Marshal(domainast.Node{
		Function: "Aggregator",
		NamedChildren: map[string]domainast.Node{
			"tableName":  {Constant: "transactions"},
			"fieldName":  {Constant: "object_id"},
			"aggregator": {Constant: "COUNT"},
			"filters": {
				Function: "List",
				Children: []domainast.Node{
					{Function: "Filter", NamedChildren: map[string]domainast.Node{"fieldName": {Constant: "merchant_id"}, "operator": {Constant: "="}, "value": {Constant: "m1"}}},
					{Function: "Filter", NamedChildren: map[string]domainast.Node{"fieldName": {Constant: "created_at"}, "operator": {Constant: ">="}, "value": {Constant: "2026-11-18T00:00:00Z"}}},
				},
			},
		},
	})
	sumFormula, _ := json.Marshal(domainast.Node{
		Function: "Aggregator",
		NamedChildren: map[string]domainast.Node{
			"tableName":  {Constant: "transactions"},
			"fieldName":  {Constant: "amount"},
			"aggregator": {Constant: "SUM"},
			"filters": {
				Function: "List",
				Children: []domainast.Node{
					{Function: "Filter", NamedChildren: map[string]domainast.Node{"fieldName": {Constant: "merchant_id"}, "operator": {Constant: "="}, "value": {Constant: "m1"}}},
					{Function: "Filter", NamedChildren: map[string]domainast.Node{"fieldName": {Constant: "created_at"}, "operator": {Constant: ">="}, "value": {Constant: "2026-11-18T00:00:00Z"}}},
				},
			},
		},
	})

	live1 := "iter-live-1"
	live2 := "iter-live-2"
	dropper := &recordingIndexDropper{}
	gc := NewIndexGCService(
		multiScenarioRepoStub{scenarios: []scenario.Scenario{
			{ID: "s1", TenantID: "t1", TriggerObjectType: "transactions", LiveIterationID: &live1},
			{ID: "s2", TenantID: "t1", TriggerObjectType: "transactions", LiveIterationID: &live2},
		}},
		gcIterationRepoStub{byScenario: map[string][]scenario.Iteration{
			"s1": {{ID: live1, ScenarioID: "s1", TenantID: "t1", Status: scenario.IterationStatusCommitted, TriggerFormula: countFormula}},
			"s2": {{ID: live2, ScenarioID: "s2", TenantID: "t1", Status: scenario.IterationStatusCommitted, TriggerFormula: sumFormula}},
		}},
		ruleListMapStub{},
		&publicationDataModelReader{
			model: ports.TenantModel{
				RecordLookupField: "object_id",
				Tables: map[string]ports.TenantModelTable{
					"transactions": {
						ID:   "tbl-1",
						Name: "transactions",
						Fields: map[string]ports.TenantModelField{
							"merchant_id": {Name: "merchant_id", Type: "string"},
							"created_at":  {Name: "created_at", Type: "timestamp"},
							"amount":      {Name: "amount", Type: "float"},
						},
					},
				},
			},
			jobs: []ports.ManagedIndexJob{
				{ID: "job-old-plain", TableName: "transactions", IndexType: "search", Status: "applied", Columns: []string{"merchant_id", "created_at"}, CompletedAt: &oldCompleted},
				{ID: "job-covering", TableName: "transactions", IndexType: "search", Status: "applied", Columns: []string{"merchant_id", "created_at"}, IncludeColumns: []string{"amount"}, CompletedAt: &oldCompleted},
				{ID: "job-orphan-old", TableName: "transactions", IndexType: "search", Status: "applied", Columns: []string{"legacy_field"}, CompletedAt: &oldCompleted},
				{ID: "job-orphan-fresh", TableName: "transactions", IndexType: "search", Status: "applied", Columns: []string{"fresh_field"}, CompletedAt: &recentCompleted},
			},
		},
		dropper,
		fixedClock{now: now},
		24*time.Hour,
	)

	report, err := gc.CollectUnreferencedAndDrop(context.Background(), "t1")
	if err != nil {
		t.Fatalf("CollectUnreferencedAndDrop() error = %v", err)
	}
	if len(dropper.dropped) != 1 || dropper.dropped[0] != "job-orphan-old" {
		t.Fatalf("dropped = %v, want [job-orphan-old] (old live-referenced plain index and fresh orphan must be retained)", dropper.dropped)
	}
	if len(report.RetainedIDs) != 3 {
		t.Fatalf("retained = %v, want 3 retained jobs", report.RetainedIDs)
	}
}
