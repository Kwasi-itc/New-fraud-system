package riverjobs

import (
	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
)

const ScreeningQueueName = "screening_dispatch"

type ScreeningArgs struct {
	Origin      platformauth.Origin `json:"originator"`
	TenantID    string              `json:"tenant_id"`
	ScreeningID string              `json:"screening_id"`
}

func (ScreeningArgs) Kind() string {
	return "screening_dispatch"
}
