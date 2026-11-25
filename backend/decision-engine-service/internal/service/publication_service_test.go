package service

import (
	"context"
	"encoding/json"
	"errors"
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

func (s *publicationDataModelReader) RetryIndexJob(context.Context, string, string) error {
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

type preparationProbe struct {
	*publicationDataModelReader
	prepared *bool
}

func (p preparationProbe) GetTenantModel(ctx context.Context, tenant string) (ports.TenantModel, error) {
	*p.prepared = true
	return p.publicationDataModelReader.GetTenantModel(ctx, tenant)
}

type publicationAuthorityBoundary struct {
	t        *testing.T
	prepared *bool
	denied   error
}

func (p publicationAuthorityBoundary) Run(context.Context, func(ports.MutationStore) error) error {
	if !*p.prepared {
		p.t.Error("publication opened its transaction before remote preparation")
	}
	return p.denied
}
func TestPublicationPreparationFinishesBeforeCurrentAuthorityTransaction(t *testing.T) {
	prepared := false
	denied := errors.New("current authority revoked")
	svc := PublicationService{txManager: publicationAuthorityBoundary{t, &prepared, denied}, scenarioRepo: scenarioRepoStub{item: scenario.Scenario{ID: "scenario", TenantID: "tenant", TriggerObjectType: "transactions"}}, iterationRepo: scenarioIterationRepoStub{iteration: scenario.Iteration{ID: "iteration", ScenarioID: "scenario", TenantID: "tenant"}}, ruleRepo: ruleRepoStub{}, dataModelReader: preparationProbe{&publicationDataModelReader{model: ports.TenantModel{Tables: map[string]ports.TenantModelTable{}}}, &prepared}}
	if _, err := svc.Publish(context.Background(), "tenant", "scenario", "iteration"); !errors.Is(err, denied) || !prepared {
		t.Fatal("publication bypassed preparation/current-authority boundary", err)
	}
}

func (s *publicationDataModelReader) CreateCoveringIndexJob(_ context.Context, _, tableID, indexType string, columns, includeColumns []string, _ string) (ports.ManagedIndexJob, error) {
	job := ports.ManagedIndexJob{
		ID:             "job-1",
		TableName:      tableNameByID(s.model, tableID),
		IndexType:      indexType,
		Status:         "pending",
		Columns:        append([]string(nil), columns...),
		IncludeColumns: append([]string(nil), includeColumns...),
	}
	s.created = append(s.created, job)
	s.jobs = append(s.jobs, job)
	return job, nil
}

func TestStartPreparationOrdersKeysCanonicallyAndExtractsIncludeColumns(t *testing.T) {
	t.Parallel()

	formula, _ := json.Marshal(domainast.Node{
		Function: ">",
		Children: []domainast.Node{
			{
				Function: "Aggregator",
				NamedChildren: map[string]domainast.Node{
					"tableName":  {Constant: "transactions"},
					"fieldName":  {Constant: "amount"},
					"aggregator": {Constant: "SUM"},
					"filters": {
						Function: "List",
						Children: []domainast.Node{
							{
								Function: "Filter",
								NamedChildren: map[string]domainast.Node{
									"tableName": {Constant: "transactions"},
									"fieldName": {Constant: "created_at"},
									"operator":  {Constant: ">="},
									"value":     {Constant: "2026-11-18T00:00:00Z"},
								},
							},
							{
								Function: "Filter",
								NamedChildren: map[string]domainast.Node{
									"tableName": {Constant: "transactions"},
									"fieldName": {Constant: "status"},
									"operator":  {Constant: "!="},
									"value":     {Constant: "void"},
								},
							},
							{
								Function: "Filter",
								NamedChildren: map[string]domainast.Node{
									"tableName": {Constant: "transactions"},
									"fieldName": {Constant: "merchant_id"},
									"operator":  {Constant: "="},
									"value":     {Constant: "m1"},
								},
							},
						},
					},
				},
			},
			{Constant: 1000},
		},
	})

	dataModel := &publicationDataModelReader{
		model: ports.TenantModel{
			Tables: map[string]ports.TenantModelTable{
				"transactions": {
					ID:   "table-1",
					Name: "transactions",
					Fields: map[string]ports.TenantModelField{
						"merchant_id": {Name: "merchant_id", Type: "string"},
						"created_at":  {Name: "created_at", Type: "timestamp"},
						"amount":      {Name: "amount", Type: "float"},
						"status":      {Name: "status", Type: "string"},
					},
				},
			},
		},
	}
	svc := PublicationService{
		scenarioRepo: scenarioRepoStub{item: scenario.Scenario{
			ID: "scenario-1", TenantID: "tenant-1", TriggerObjectType: "transactions",
		}},
		iterationRepo: scenarioIterationRepoStub{iteration: scenario.Iteration{
			ID: "iteration-1", ScenarioID: "scenario-1", TenantID: "tenant-1", TriggerFormula: formula,
		}},
		ruleRepo:        ruleRepoStub{},
		dataModelReader: dataModel,
	}

	if _, err := svc.StartPreparation(context.Background(), "tenant-1", "scenario-1", "iteration-1"); err != nil {
		t.Fatalf("StartPreparation() error = %v", err)
	}
	if len(dataModel.created) != 1 {
		t.Fatalf("created jobs = %d, want 1", len(dataModel.created))
	}
	job := dataModel.created[0]
	if len(job.Columns) != 2 || job.Columns[0] != "merchant_id" || job.Columns[1] != "created_at" {
		t.Fatalf("key columns = %v, want [merchant_id created_at]", job.Columns)
	}
	if len(job.IncludeColumns) != 2 || job.IncludeColumns[0] != "amount" || job.IncludeColumns[1] != "status" {
		t.Fatalf("include columns = %v, want [amount status]", job.IncludeColumns)
	}
}

func TestStartPreparationIgnoresOrSubtreesAndSubsumesAppliedIndexes(t *testing.T) {
	t.Parallel()

	node := domainast.Node{
		Function: "Aggregator",
		NamedChildren: map[string]domainast.Node{
			"tableName":  {Constant: "transactions"},
			"fieldName":  {Constant: "amount"},
			"aggregator": {Constant: "SUM"},
			"filters": {
				Function: "or",
				Children: []domainast.Node{
					{
						Function: "Filter",
						NamedChildren: map[string]domainast.Node{
							"fieldName": {Constant: "merchant_id"},
							"operator":  {Constant: "="},
							"value":     {Constant: "m1"},
						},
					},
				},
			},
		},
	}
	model := ports.TenantModel{
		Tables: map[string]ports.TenantModelTable{
			"transactions": {
				ID:   "t1",
				Name: "transactions",
				Fields: map[string]ports.TenantModelField{
					"merchant_id": {Name: "merchant_id", Type: "string"},
					"amount":      {Name: "amount", Type: "float"},
				},
			},
		},
	}
	if _, ok := aggregatorIndexRequirement(model, "transactions", node); ok {
		t.Fatal("expected or-only filter subtree not to emit composite B-tree index requirement")
	}

	applied := []ports.ManagedIndexJob{{
		TableName:      "transactions",
		Status:         "applied",
		Columns:        []string{"merchant_id", "created_at", "currency"},
		IncludeColumns: []string{"amount", "status"},
	}}
	req := indexRequirement{
		TableName:      "transactions",
		Columns:        []string{"merchant_id", "created_at"},
		IncludeColumns: []string{"amount"},
	}
	if !requirementSatisfiedByJobs(applied, req) {
		t.Fatal("expected wider applied index to subsume narrower requirement")
	}
}

func TestStartPreparationEnforcesPerTableIndexCapAndAggregatePolicy(t *testing.T) {
	t.Parallel()

	formula, _ := json.Marshal(domainast.Node{
		Function: ">",
		Children: []domainast.Node{
			{
				Function: "Aggregator",
				NamedChildren: map[string]domainast.Node{
					"tableName":  {Constant: "transactions"},
					"fieldName":  {Constant: "amount"},
					"aggregator": {Constant: "SUM"},
					"filters": {
						Function: "List",
						Children: []domainast.Node{{
							Function: "Filter",
							NamedChildren: map[string]domainast.Node{
								"fieldName": {Constant: "merchant_id"},
								"operator":  {Constant: "="},
								"value":     {Constant: "m1"},
							},
						}},
					},
				},
			},
			{Constant: 100},
		},
	})

	dataModel := &publicationDataModelReader{
		model: ports.TenantModel{
			Tables: map[string]ports.TenantModelTable{
				"transactions": {
					ID:   "table-1",
					Name: "transactions",
					Fields: map[string]ports.TenantModelField{
						"merchant_id": {Name: "merchant_id", Type: "string"},
						"amount":      {Name: "amount", Type: "float"},
					},
				},
			},
		},
		jobs: []ports.ManagedIndexJob{
			{ID: "j1", TableName: "transactions", IndexType: "search", Status: "applied", Columns: []string{"a"}},
			{ID: "j2", TableName: "transactions", IndexType: "search", Status: "applied", Columns: []string{"b"}},
		},
	}
	svc := PublicationService{
		scenarioRepo: scenarioRepoStub{item: scenario.Scenario{
			ID: "s1", TenantID: "t1", TriggerObjectType: "transactions",
		}},
		iterationRepo: scenarioIterationRepoStub{iteration: scenario.Iteration{
			ID: "i1", ScenarioID: "s1", TenantID: "t1", TriggerFormula: formula,
		}},
		ruleRepo:        ruleRepoStub{},
		dataModelReader: dataModel,
	}
	svc.SetMaxSearchIndexesPerTable(2)
	if _, err := svc.StartPreparation(context.Background(), "t1", "s1", "i1"); err == nil {
		t.Fatal("expected per-table index cap error")
	}

	svc.SetAggregatePushdownPolicy("enabled", []string{"count"})
	if _, err := svc.GetPreparationStatus(context.Background(), "t1", "s1", "i1"); err == nil {
		t.Fatal("expected disabled aggregate policy error for SUM when only count is enabled")
	}
}
