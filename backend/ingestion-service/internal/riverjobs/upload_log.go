package riverjobs

import "github.com/google/uuid"

const UploadLogQueueName = "upload_logs"

type UploadLogArgs struct {
	UploadLogID uuid.UUID `json:"upload_log_id"`
}

func (UploadLogArgs) Kind() string {
	return "upload_log_execute"
}
