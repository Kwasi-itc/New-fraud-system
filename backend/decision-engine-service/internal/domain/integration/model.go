package integration

type EvaluationRequest struct {
	TenantID   string
	ObjectType string
	ObjectID   string
}

type ServiceInfo struct {
	DataModelServiceURL string
	IngestionServiceURL string
}
