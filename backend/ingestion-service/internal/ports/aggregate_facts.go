package ports

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrAggregateFactMutationUnsupported = errors.New("updates and deletes are not supported for fact-enabled event data")

type AggregateFactDefinition struct {
	ID               string
	TenantID         string
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

type AggregateFactPendingBucket struct {
	Key       string
	Dimension string
	ExpiresAt int64
}

type AggregateFactDelta struct {
	Key       string
	Dimension string
	Sum       float64
	Count     int64
	ExpiresAt int64
}

type PreparedAggregateFactWrite struct {
	TenantID         uuid.UUID
	ObjectType       string
	Marker           string
	Manifest         string
	Owner            string
	Definitions      []AggregateFactDefinition
	Records          []map[string]any
	Deltas           []AggregateFactDelta
	PendingBuckets   []AggregateFactPendingBucket
	Prepared         bool
	AlreadyApplied   bool
	OwnsMarker       bool
	PendingRetry     bool
	MaintenanceGuard bool
}

type AggregateFactWriter interface {
	Prepare(ctx context.Context, tenantID uuid.UUID, objectType string, records []map[string]any, requestMarker string) (PreparedAggregateFactWrite, error)
	Renew(ctx context.Context, write PreparedAggregateFactWrite) error
	Commit(ctx context.Context, write PreparedAggregateFactWrite) error
	Confirm(ctx context.Context, write PreparedAggregateFactWrite) error
	Abort(ctx context.Context, write PreparedAggregateFactWrite) error
}

type AggregateFactDefinitionStatus struct {
	ID               string `json:"id"`
	TableName        string `json:"table_name"`
	Signature        string `json:"signature"`
	Version          int64  `json:"version"`
	BackfillRequired bool   `json:"backfill_required"`
	CoverageReady    bool   `json:"coverage_ready"`
	Dirty            bool   `json:"dirty"`
}

type AggregateFactBackfillStatus struct {
	TenantID        string                          `json:"tenant_id"`
	Ready           bool                            `json:"ready"`
	DefinitionCount int                             `json:"definition_count"`
	PendingReceipts int64                           `json:"pending_receipts"`
	Definitions     []AggregateFactDefinitionStatus `json:"definitions"`
}

type AggregateFactBackfillResult struct {
	TenantID           string                                  `json:"tenant_id"`
	Rebuilt            bool                                    `json:"rebuilt"`
	DefinitionsRebuilt int                                     `json:"definitions_rebuilt"`
	GroupsWritten      int64                                   `json:"groups_written"`
	BucketKeysWritten  int64                                   `json:"bucket_keys_written"`
	StartedAt          time.Time                               `json:"started_at"`
	CompletedAt        time.Time                               `json:"completed_at"`
	DefinitionResults  []AggregateFactDefinitionBackfillResult `json:"definition_results"`
	Status             AggregateFactBackfillStatus             `json:"status"`
}

type AggregateFactDefinitionBackfillResult struct {
	ID                string    `json:"id"`
	TableName         string    `json:"table_name"`
	Signature         string    `json:"signature"`
	DimensionFields   []string  `json:"dimension_fields"`
	MeasureField      string    `json:"measure_field"`
	MaxWindowSeconds  int64     `json:"max_window_seconds"`
	GroupsWritten     int64     `json:"groups_written"`
	BucketKeysWritten int64     `json:"bucket_keys_written"`
	StartedAt         time.Time `json:"started_at"`
	CompletedAt       time.Time `json:"completed_at"`
	DurationMS        int64     `json:"duration_ms"`
}

type AggregateFactBackfiller interface {
	Status(ctx context.Context, tenantID uuid.UUID) (AggregateFactBackfillStatus, error)
	Backfill(ctx context.Context, tenantID uuid.UUID, force bool) (AggregateFactBackfillResult, error)
}
