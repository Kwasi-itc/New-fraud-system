package riverjobs

const DatasetJobQueueName = "dataset_update_jobs"

type DatasetJobArgs struct {
	TenantID string `json:"tenant_id"`
	JobID    string `json:"job_id"`
}

func (DatasetJobArgs) Kind() string {
	return "dataset_update_job"
}
