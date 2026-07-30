package ingestion

import (
	"time"

	"github.com/google/uuid"
)

type PublishedDataModel struct {
	TenantID            uuid.UUID
	RevisionID          string
	TenantStatus        string
	Writable            bool
	RecordLookupField   string
	PartialUpdates      bool
	ManagedSystemFields []string
	Tables              map[string]ObjectSchema
	PhysicalSchemaName  string
	LogicalBuckets      []LogicalBucketDefinition
}

type LogicalBucketDefinition struct {
	ID                 uuid.UUID
	TableID            uuid.UUID
	TimestampFieldID   uuid.UUID
	TimestampFieldName string
	Grain              string
	Timezone           string
	SealDelay          time.Duration
	DefinitionVersion  int
	Status             string
	CacheEligibleAt    *time.Time
	MaintenanceUntil   *time.Time
}

func (m PublishedDataModel) MaintainedBucketsForTable(tableID uuid.UUID, now time.Time) []LogicalBucketDefinition {
	out := make([]LogicalBucketDefinition, 0, len(m.LogicalBuckets))
	for _, item := range m.LogicalBuckets {
		if item.TableID != tableID {
			continue
		}
		switch item.Status {
		case "activating", "active":
			out = append(out, item)
		case "retiring":
			if item.MaintenanceUntil == nil || now.Before(*item.MaintenanceUntil) {
				out = append(out, item)
			}
		}
	}
	return out
}

type ObjectSchema struct {
	ID           uuid.UUID
	Name         string
	Description  string
	Alias        string
	SemanticType string
	CaptionField string
	Archived     bool
	Fields       map[string]FieldSchema
}

type FieldSchema struct {
	ID          uuid.UUID
	Name        string
	Description string
	DataType    string
	Nullable    bool
	IsEnum      bool
	IsUnique    bool
	Archived    bool
	EnumValues  []EnumValue
}

type EnumValue struct {
	ID    uuid.UUID
	Value string
	Label string
}
