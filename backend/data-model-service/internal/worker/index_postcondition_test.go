package worker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/google/uuid"
)

type indexInspection struct {
	state datamodel.ManagedIndexState
	err   error
}
type postconditionSchema struct {
	ports.SchemaManager
	results        []indexInspection
	reads, creates int
}

func (s *postconditionSchema) GetManagedIndexState(context.Context, tenant.Tenant, datamodel.Table, datamodel.IndexJob) (datamodel.ManagedIndexState, error) {
	if s.reads >= len(s.results) {
		return datamodel.ManagedIndexState{}, errors.New("unexpected inspection")
	}
	result := s.results[s.reads]
	s.reads++
	return result.state, result.err
}
func (s *postconditionSchema) CreateManagedIndex(context.Context, tenant.Tenant, datamodel.Table, datamodel.IndexJob) error {
	s.creates++
	return nil
}

func TestRunnerRequiresIndexPostcondition(t *testing.T) {
	inspectionFailure := errors.New("inspection failed after DDL")
	outcomeFailure := errors.New("outcome persistence failed")
	for _, scenario := range []string{"created", "still_missing", "changed_identity", "empty_identity", "inspection_error", "exhausted", "outcome_error", "existing_empty_identity"} {
		t.Run(scenario, func(t *testing.T) {
			tenantID, tableID := uuid.New(), uuid.New()
			job := datamodel.IndexJob{ID: uuid.New(), TenantID: tenantID, TableID: &tableID, TableName: "records", AttemptCount: 1}
			jobs := &stubWorkerIndexJobRepository{claimed: &job}
			history := &stubWorkerSchemaChangeRepository{}
			schema := &postconditionSchema{results: []indexInspection{{state: datamodel.ManagedIndexState{Name: "expected"}}, {state: datamodel.ManagedIndexState{Name: "expected", Exists: true}}}}
			wantErr := ErrManagedIndexUnverified
			var finishErr error
			switch scenario {
			case "created":
				wantErr = nil
			case "still_missing":
				schema.results[1].state.Exists = false
			case "changed_identity":
				schema.results[1].state.Name = "other"
			case "empty_identity":
				schema.results[1].state.Name = ""
			case "inspection_error":
				schema.results[1].err = inspectionFailure
				wantErr = inspectionFailure
			case "exhausted":
				job.AttemptCount = 3
				schema.results[1].state.Exists = false
			case "outcome_error":
				schema.results[1].state.Exists = false
				finishErr = outcomeFailure
			case "existing_empty_identity":
				schema.results[0].state = datamodel.ManagedIndexState{Exists: true}
			}
			runner := NewRunner(slog.New(slog.NewTextHandler(io.Discard, nil)), stubWorkerTenantRepository{record: tenant.Tenant{ID: tenantID}}, stubWorkerTableRepository{table: datamodel.Table{ID: tableID, TenantID: tenantID, Name: "records"}}, stubWorkerExecution{jobs, history, finishErr}, schema, stubWorkerIDGenerator{uuid.New()}, fixedClock{time.Now().UTC()}, 3)
			err := runner.RunJob(context.Background(), job.ID)
			if !errors.Is(err, wantErr) {
				t.Fatalf("error %v want %v", err, wantErr)
			}
			if finishErr != nil {
				if !errors.Is(err, finishErr) || len(history.changes) != 0 {
					t.Fatalf("outcome failure lost %v", err)
				}
				return
			}
			if scenario == "created" {
				if len(jobs.appliedIDs) != 1 {
					t.Fatal("verified index not applied")
				}
				var details struct {
					IndexName string `json:"index_name"`
				}
				if e := json.Unmarshal(history.changes[0].Details, &details); e != nil || details.IndexName != "expected" {
					t.Fatalf("audit name %s %v", details.IndexName, e)
				}
			} else {
				if len(jobs.appliedIDs) != 0 {
					t.Fatal("unverified index applied")
				}
				if scenario == "exhausted" {
					if len(jobs.failedIDs) != 1 {
						t.Fatal("exhausted postcondition not failed")
					}
				} else if len(jobs.rescheduledIDs) != 1 {
					t.Fatal("postcondition failure not rescheduled")
				}
			}
			wantReads, wantCreates := 2, 1
			if scenario == "existing_empty_identity" {
				wantReads, wantCreates = 1, 0
			}
			if schema.reads != wantReads || schema.creates != wantCreates {
				t.Fatalf("reads=%d creates=%d", schema.reads, schema.creates)
			}
		})
	}
}
