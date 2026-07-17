package riverjobs

import "github.com/google/uuid"

const IndexJobQueueName = "index_jobs"

type IndexJobArgs struct {
	IndexJobID uuid.UUID `json:"index_job_id"`
}

func (IndexJobArgs) Kind() string {
	return "index_job_execute"
}
