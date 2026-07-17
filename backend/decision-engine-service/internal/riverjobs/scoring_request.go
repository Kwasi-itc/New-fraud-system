package riverjobs

const ScoringRequestQueueName = "scoring_requests"

type ScoringRequestArgs struct {
	TenantID  string `json:"tenant_id"`
	RequestID string `json:"request_id"`
}

func (ScoringRequestArgs) Kind() string {
	return "scoring_request"
}
