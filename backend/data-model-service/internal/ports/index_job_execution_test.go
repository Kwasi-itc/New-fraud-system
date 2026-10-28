package ports

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestIndexJobOutcomeValidation(t *testing.T) {
	job := datamodel.IndexJob{ID: uuid.New(), TenantID: uuid.New(), AttemptCount: 1, Status: datamodel.IndexJobStatusRunning}
	valid := IndexJobOutcome{Job: job, Change: datamodel.SchemaChange{ID: uuid.New(), TenantID: job.TenantID, ResourceID: job.ID, ResourceType: "index_job", Operation: "apply_index_job", Status: "applied", Details: []byte(`{}`), CreatedAt: time.Now().UTC()}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*IndexJobOutcome){
		"zero_job":           func(o *IndexJobOutcome) { o.Job.ID = uuid.Nil },
		"zero_tenant":        func(o *IndexJobOutcome) { o.Job.TenantID = uuid.Nil },
		"zero_attempt":       func(o *IndexJobOutcome) { o.Job.AttemptCount = 0 },
		"not_running":        func(o *IndexJobOutcome) { o.Job.Status = datamodel.IndexJobStatusPending },
		"history_identity":   func(o *IndexJobOutcome) { o.Change.ID = uuid.Nil },
		"wrong_tenant":       func(o *IndexJobOutcome) { o.Change.TenantID = uuid.New() },
		"wrong_job":          func(o *IndexJobOutcome) { o.Change.ResourceID = uuid.New() },
		"wrong_resource":     func(o *IndexJobOutcome) { o.Change.ResourceType = "field" },
		"zero_timestamp":     func(o *IndexJobOutcome) { o.Change.CreatedAt = time.Time{} },
		"invalid_json":       func(o *IndexJobOutcome) { o.Change.Details = []byte(`{`) },
		"invalid_status":     func(o *IndexJobOutcome) { o.Change.Status = "running" },
		"wrong_operation":    func(o *IndexJobOutcome) { o.Change.Operation = "fail_index_job" },
		"success_with_error": func(o *IndexJobOutcome) { o.ErrorMessage = "failure" },
		"success_with_retry": func(o *IndexJobOutcome) { o.RetryDelivery = true },
	} {
		t.Run(name, func(t *testing.T) {
			o := valid
			mutate(&o)
			if err := o.Validate(); err == nil {
				t.Fatal("invalid outcome accepted")
			}
		})
	}
}
