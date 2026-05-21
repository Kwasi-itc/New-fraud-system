package ingestion

import "github.com/google/uuid"

type PublishedDataModel struct {
	TenantID            uuid.UUID
	RevisionID          string
	TenantStatus        string
	Writable            bool
	RecordLookupField   string
	PartialUpdates      bool
	ManagedSystemFields []string
	Tables              map[string]ObjectSchema
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
