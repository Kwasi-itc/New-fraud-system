package service

import (
	"context"
	"encoding/json"
	"testing"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type publicationDataModelReader struct {
	model   ports.TenantModel
	jobs    []ports.ManagedIndexJob
	created []ports.ManagedIndexJob
}

func (s *publicationDataModelReader) GetTenantModel(context.Context, string) (ports.TenantModel, error) {
	return s.model, nil
}

func (s *publicationDataModelReader) CreateIndexJob(_ context.Context, _, tableID, indexType string, columns []string, _ string) (ports.ManagedIndexJob, error) {
	job := ports.ManagedIndexJob{
		ID:        "index-job-1",
		TableName: tableNameByID(s.model, tableID),
		IndexType: indexType,
		Status:    "pending",
		Columns:   append([]string(nil), columns...),
	}
	s.created = append(s.created, job)
	s.jobs = append(s.jobs, job)
	return job, nil
}

func (s *publicationDataModelReader) ListIndexJobs(context.Context, string) ([]ports.ManagedIndexJob, error) {
	return append([]ports.ManagedIndexJob(nil), s.jobs...), nil
}

func (s *publicationDataModelReader) RetryIndexJob(context.Context, string) error {
	return nil
}

func TestStartPreparationCreatesSearchIndexForAggregatorFilters(t *testing.T) {
	formula, err := json.Marshal(domainast.Node{
		Function: "Aggregator",
		NamedChildren: map[string]domainast.Node{
			"tableName": {Constant: "transactions"},
			"filters": {
				Function: "List",
				Children: []domainast.Node{
					{
						Function: "Filter",
						NamedChildren: map[string]domainast.Node{
							"tableName": {Constant: "transactions"},
							"fieldName": {Constant: "customer_id"},
						},
					},
					{
						Function: "Filter",
						NamedChildren: map[string]domainast.Node{
							"tableName": {Constant: "transactions"},
							"fieldName": {Constant: "created_at"},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal formula: %v", err)
	}

	dataModel := &publicationDataModelReader{model: ports.TenantModel{
		Tables: map[string]ports.TenantModelTable{
			"transactions": {
				ID:   "table-transactions",
				Name: "transactions",
				Fields: map[string]ports.TenantModelField{
					"customer_id": {Name: "customer_id"},
					"created_at":  {Name: "created_at"},
				},
			},
		},
	}}
	service := PublicationService{
		scenarioRepo: scenarioRepoStub{item: scenario.Scenario{
			ID: "scenario-1", TenantID: "tenant-1", TriggerObjectType: "transactions",
		}},
		iterationRepo: scenarioIterationRepoStub{iteration: scenario.Iteration{
			ID: "iteration-1", ScenarioID: "scenario-1", TenantID: "tenant-1", TriggerFormula: formula,
		}},
		ruleRepo:        ruleRepoStub{},
		dataModelReader: dataModel,
	}

	status, err := service.StartPreparation(context.Background(), "tenant-1", "scenario-1", "iteration-1")
	if err != nil {
		t.Fatalf("StartPreparation() error = %v", err)
	}
	if len(dataModel.created) != 1 {
		t.Fatalf("created index jobs = %d, want 1", len(dataModel.created))
	}
	job := dataModel.created[0]
	if job.IndexType != "search" {
		t.Fatalf("index type = %q, want search", job.IndexType)
	}
	if job.TableName != "transactions" {
		t.Fatalf("table name = %q, want transactions", job.TableName)
	}
	if len(job.Columns) != 2 || job.Columns[0] != "customer_id" || job.Columns[1] != "created_at" {
		t.Fatalf("columns = %#v, want [customer_id created_at]", job.Columns)
	}
	if !status.PreparationRequired || !status.PreparationStarted || status.PreparationFinished || status.PendingItems != 1 {
		t.Fatalf("preparation status = %#v, want one pending item", status)
	}
}

func tableNameByID(model ports.TenantModel, tableID string) string {
	for _, table := range model.Tables {
		if table.ID == tableID {
			return table.Name
		}
	}
	return ""
}

var _ ports.DataModelReader = (*publicationDataModelReader)(nil)
