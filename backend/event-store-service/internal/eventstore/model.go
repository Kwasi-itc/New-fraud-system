package eventstore

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type Config struct {
	Port                    string
	ClickHouseURL           string
	ClickHouseDatabase      string
	ClickHouseUser          string
	ClickHousePassword      string
	ValkeyAddress           string
	DisableAggregateFacts   bool
	FeatureNamespace        string
	FeatureMaxKeys          int
	FeatureMaxKeysPerTenant int
	FeatureAdmissionHits    int
	FeatureSlowQueryMS      int
	FeatureTTL              time.Duration
	HTTPTimeout             time.Duration
	MaxConns                int
	MaxIdleConns            int
	IdleConnTimeout         time.Duration
}

type Event struct {
	EventID     string         `json:"event_id"`
	ObjectID    string         `json:"object_id"`
	EventTime   time.Time      `json:"event_time"`
	IngestedAt  time.Time      `json:"ingested_at"`
	RequestHash string         `json:"request_hash"`
	Payload     map[string]any `json:"payload"`
}

type FieldContract struct {
	DataType                string   `json:"data_type"`
	Nullable                bool     `json:"nullable"`
	IsProjection            bool     `json:"is_projection,omitempty"`
	AggregationMode         string   `json:"aggregation_mode,omitempty"`
	AggregationColdBehavior string   `json:"aggregation_cold_behavior,omitempty"`
	AggregationDefaultValue *float64 `json:"aggregation_default_value,omitempty"`
}

type TableContract struct {
	TenantID       string                   `json:"tenant_id"`
	TableID        string                   `json:"table_id"`
	ObjectType     string                   `json:"object_type"`
	RevisionID     string                   `json:"revision_id"`
	SchemaRevision string                   `json:"schema_revision"`
	EventTimeField string                   `json:"event_time_field"`
	Fields         map[string]FieldContract `json:"fields"`
}

type EventWriteRequest struct {
	Table TableContract `json:"table"`
	Event Event         `json:"event"`
}

type EventBatch struct {
	Table  TableContract `json:"table"`
	Events []Event       `json:"events"`
}

type RecordRequest struct {
	Table    TableContract `json:"table"`
	ObjectID string        `json:"object_id,omitempty"`
	Field    string        `json:"field,omitempty"`
	Value    string        `json:"value,omitempty"`
	Limit    int           `json:"limit,omitempty"`
}

type AggregateRequest struct {
	Table     TableContract    `json:"table"`
	Aggregate string           `json:"aggregate"`
	Field     string           `json:"field"`
	Filter    *AggregateFilter `json:"filter,omitempty"`
}

type AggregateFilter struct {
	Kind     string            `json:"kind,omitempty"`
	Operator string            `json:"operator,omitempty"`
	Children []AggregateFilter `json:"children,omitempty"`
	Field    string            `json:"field,omitempty"`
	Op       string            `json:"op,omitempty"`
	Value    any               `json:"value,omitempty"`
}

func (event Event) validate(table TableContract) error {
	if strings.TrimSpace(event.EventID) == "" || strings.TrimSpace(event.ObjectID) == "" {
		return fmt.Errorf("event_id and object_id are required")
	}
	if event.EventTime.IsZero() {
		return fmt.Errorf("event_time is required")
	}
	if event.IngestedAt.IsZero() {
		return fmt.Errorf("ingested_at is required")
	}
	if event.Payload == nil {
		return fmt.Errorf("payload is required")
	}
	if payloadObjectID, ok := event.Payload["object_id"]; !ok || fmt.Sprint(payloadObjectID) != event.ObjectID {
		return fmt.Errorf("payload object_id must match object_id")
	}
	for name := range event.Payload {
		if _, ok := table.Fields[name]; !ok {
			return fmt.Errorf("payload field %s is not in the table contract", name)
		}
	}
	for name, field := range table.Fields {
		if name == "updated_at" || field.Nullable {
			continue
		}
		if value, ok := event.Payload[name]; !ok || value == nil {
			return fmt.Errorf("non-null field %s is missing", name)
		}
	}
	return nil
}

func (table TableContract) validate() error {
	if !uuidPattern.MatchString(table.TenantID) {
		return fmt.Errorf("invalid tenant_id")
	}
	if !uuidPattern.MatchString(table.TableID) {
		return fmt.Errorf("invalid table_id")
	}
	if !identifierPattern.MatchString(table.ObjectType) {
		return fmt.Errorf("invalid object_type")
	}
	if strings.TrimSpace(table.RevisionID) == "" || strings.TrimSpace(table.SchemaRevision) == "" {
		return fmt.Errorf("revision_id and schema_revision are required")
	}
	if !identifierPattern.MatchString(table.EventTimeField) {
		return fmt.Errorf("invalid event_time_field")
	}
	if len(table.Fields) == 0 {
		return fmt.Errorf("fields are required")
	}
	for name, field := range table.Fields {
		if !identifierPattern.MatchString(name) {
			return fmt.Errorf("invalid field %q", name)
		}
		if _, err := clickHouseType(field); err != nil {
			return fmt.Errorf("field %s: %w", name, err)
		}
		if err := validateFieldAggregationPolicy(name, field); err != nil {
			return err
		}
	}
	objectID, ok := table.Fields["object_id"]
	if !ok || objectID.DataType != "string" || objectID.Nullable {
		return fmt.Errorf("object_id must be a non-null string field")
	}
	eventTime, ok := table.Fields[table.EventTimeField]
	if !ok || eventTime.DataType != "timestamp" || eventTime.Nullable {
		return fmt.Errorf("event_time_field must reference a non-null timestamp field")
	}
	return nil
}

func canonicalJSON(value any) []byte {
	body, _ := json.Marshal(value)
	return body
}
