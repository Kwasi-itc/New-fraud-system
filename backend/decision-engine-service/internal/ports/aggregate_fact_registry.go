package ports

import "context"

type AggregateFactDefinition struct {
	ID               string
	TenantID         string
	TableID          string
	TableName        string
	Signature        string
	DimensionFields  []string
	EventTimeField   string
	MeasureField     string
	NeedsSum         bool
	NeedsCount       bool
	MaxWindowSeconds int64
	MinuteEnabled    bool
	BackfillRequired bool
	CoverageToken    string
	Version          int64
	RegistryVersion  int64
}

type AggregateFactRegistry interface {
	SyncScenario(ctx context.Context, tenantID, scenarioID, iterationID string, definitions []AggregateFactDefinition) error
	RemoveScenario(ctx context.Context, tenantID, scenarioID string) error
	ListActive(ctx context.Context, tenantID, tableName string) ([]AggregateFactDefinition, error)
}
