package ingestion

import "time"

type RollupStatus string

const (
	RollupStatusCapturing   RollupStatus = "capturing"
	RollupStatusBackfilling RollupStatus = "backfilling"
	RollupStatusReady       RollupStatus = "ready"
	RollupStatusDisabled    RollupStatus = "disabled"
)

type RollupValueType string

const (
	RollupValueTypeInt         RollupValueType = "int"
	RollupValueTypeFloat       RollupValueType = "float"
	RollupValueTypeLookupCount RollupValueType = "lookup_count"
)

const (
	RollupResolution15m int32 = 900
	RollupResolution1h  int32 = 3600
	RollupResolution1d  int32 = 86400

	RollupRetention15m = 14 * 24 * time.Hour
	RollupRetention1h  = 90 * 24 * time.Hour
	RollupRetention1d  = 3 * 365 * 24 * time.Hour
)

var RollupResolutions = []int32{
	RollupResolution15m,
	RollupResolution1h,
	RollupResolution1d,
}

func RollupRetentionForResolution(resolution int32) time.Duration {
	switch resolution {
	case RollupResolution15m:
		return RollupRetention15m
	case RollupResolution1h:
		return RollupRetention1h
	case RollupResolution1d:
		return RollupRetention1d
	default:
		return 0
	}
}

func AlignRollupBucketUTC(t time.Time, resolution int32) time.Time {
	u := t.UTC()
	sec := int64(resolution)
	if sec <= 0 {
		return u
	}
	unix := u.Unix()
	aligned := unix - ((unix%sec + sec) % sec)
	return time.Unix(aligned, 0).UTC()
}

type RollupDefinition struct {
	TenantID         string          `json:"tenant_id"`
	ID               string          `json:"id"`
	TableName        string          `json:"table_name"`
	GroupField       string          `json:"group_field"`
	TimeField        string          `json:"time_field"`
	ValueField       string          `json:"value_field"`
	ValueType        RollupValueType `json:"value_type"`
	ModelVersion     string          `json:"model_version"`
	Status           RollupStatus    `json:"status"`
	BackfillSnapshot *string         `json:"backfill_snapshot,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

func (d RollupDefinition) IsCapturingActive() bool {
	return d.Status == RollupStatusCapturing ||
		d.Status == RollupStatusBackfilling ||
		d.Status == RollupStatusReady
}

type RollupDelta struct {
	TenantID     string
	ID           int64
	DefinitionID string
	XID          string
	GroupValue   string
	EventTime    time.Time
	DRows        int64
	DValueCount  int64
	DValueSum    string
}

type RollupBucket struct {
	TenantID     string
	DefinitionID string
	Resolution   int32
	GroupValue   string
	BucketStart  time.Time
	Rows         int64
	ValueCount   int64
	ValueSum     string
}
