package riverjobs

const ScreeningQueueName = "screening_dispatch"

type ScreeningArgs struct {
	TenantID    string `json:"tenant_id"`
	ScreeningID string `json:"screening_id"`
}

func (ScreeningArgs) Kind() string {
	return "screening_dispatch"
}
