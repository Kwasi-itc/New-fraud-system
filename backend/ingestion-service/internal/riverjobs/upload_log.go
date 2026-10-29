package riverjobs

import "github.com/google/uuid"

const UploadLogQueueName = "upload_logs"

type UploadLogArgs struct {
	UploadLogID    uuid.UUID `json:"upload_log_id"`
	PayloadVersion int       `json:"payload_version,omitempty"`
	RetryAttempt   int       `json:"retry_attempt,omitempty"`
}

func (UploadLogArgs) Kind() string {
	return "upload_log_execute"
}
