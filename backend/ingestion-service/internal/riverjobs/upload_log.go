package riverjobs

import (
	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
)

import "github.com/google/uuid"

const UploadLogQueueName = "upload_logs"

type UploadLogArgs struct {
	Origin      platformauth.Origin `json:"originator"`
	UploadLogID uuid.UUID           `json:"upload_log_id"`
}

func (UploadLogArgs) Kind() string {
	return "upload_log_execute"
}
