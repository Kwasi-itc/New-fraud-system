package riverjobs

import (
	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
)

const ScoringRequestQueueName = "scoring_requests"

type ScoringRequestArgs struct {
	Origin    platformauth.Origin `json:"originator"`
	TenantID  string              `json:"tenant_id"`
	RequestID string              `json:"request_id"`
}

func (ScoringRequestArgs) Kind() string {
	return "scoring_request"
}
