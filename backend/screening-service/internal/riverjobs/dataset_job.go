package riverjobs

import (
	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
)

const DatasetJobQueueName = "dataset_update_jobs"

type DatasetJobArgs struct {
	Origin   platformauth.Origin `json:"originator"`
	TenantID string              `json:"tenant_id"`
	JobID    string              `json:"job_id"`
}

func (DatasetJobArgs) Kind() string {
	return "dataset_update_job"
}
