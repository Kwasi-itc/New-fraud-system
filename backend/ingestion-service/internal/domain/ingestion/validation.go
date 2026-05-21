package ingestion

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

type Mode string

const (
	ModeCreate Mode = "create"
	ModePatch  Mode = "patch"
)

type ValidationError struct {
	ObjectID string `json:"object_id,omitempty"`
	Field    string `json:"field"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

type RecordResult struct {
	ObjectID   string `json:"object_id"`
	Action     string `json:"action"`
	RevisionID string `json:"revision_id"`
	Replayed   bool   `json:"replayed"`
}

type UploadLogStatus string

const (
	UploadLogStatusPending    UploadLogStatus = "pending"
	UploadLogStatusUploaded   UploadLogStatus = "uploaded"
	UploadLogStatusProcessing UploadLogStatus = "processing"
	UploadLogStatusCompleted  UploadLogStatus = "completed"
	UploadLogStatusFailed     UploadLogStatus = "failed"
)

type IngestionAudit struct {
	ID              string
	TenantID        string
	ObjectType      string
	ObjectID        string
	Mode            Mode
	RevisionID      string
	Status          string
	Payload         []byte
	ValidationError []byte
	IdempotencyKey  *string
	CreatedAt       time.Time
}

type OutboxEvent struct {
	ID            string
	TenantID      string
	EventType     string
	AggregateType string
	AggregateKey  string
	Payload       []byte
	Status        string
	CreatedAt     time.Time
}

type IdempotencyKey struct {
	TenantID        string
	Key             string
	RequestHash     string
	ResponseKind    string
	ResponsePayload []byte
	CreatedAt       time.Time
}

type UploadLog struct {
	ID             string
	TenantID       string
	ObjectType     string
	Mode           Mode
	Filename       string
	ContentType    string
	Status         UploadLogStatus
	TotalRows      int
	SuccessfulRows int
	FailedRows     int
	AttemptCount   int
	ErrorMessage   *string
	Payload        []byte
	RequestedAt    time.Time
	StartedAt      *time.Time
	CompletedAt    *time.Time
}

func ValidateRecord(model PublishedDataModel, objectType string, payload map[string]any, mode Mode) (map[string]any, string, []ValidationError) {
	table, ok := model.Tables[objectType]
	if !ok || table.Archived {
		return nil, "", []ValidationError{{
			Field:   "object_type",
			Code:    "unknown_object_type",
			Message: fmt.Sprintf("object type %s is not available for ingestion", objectType),
		}}
	}

	normalized := make(map[string]any, len(payload))
	errors := make([]ValidationError, 0)
	lookupField := model.RecordLookupField
	if lookupField == "" {
		lookupField = "object_id"
	}

	for fieldName := range payload {
		if fieldName == lookupField {
			continue
		}
		if fieldName != lookupField && isManagedField(model.ManagedSystemFields, fieldName) {
			errors = append(errors, ValidationError{
				Field:   fieldName,
				Code:    "managed_field",
				Message: fmt.Sprintf("field %s is managed by the platform and cannot be ingested directly", fieldName),
			})
			continue
		}
		field, exists := table.Fields[fieldName]
		if !exists || field.Archived {
			errors = append(errors, ValidationError{
				Field:   fieldName,
				Code:    "unknown_field",
				Message: fmt.Sprintf("field %s is not defined on object type %s", fieldName, objectType),
			})
			continue
		}

		value, err := normalizeFieldValue(field, payload[fieldName], mode)
		if err != nil {
			errors = append(errors, ValidationError{
				Field:   fieldName,
				Code:    "invalid_value",
				Message: err.Error(),
			})
			continue
		}
		normalized[fieldName] = value
	}

	objectIDRaw, ok := payload[lookupField]
	if !ok {
		errors = append(errors, ValidationError{
			Field:   lookupField,
			Code:    "missing_required",
			Message: fmt.Sprintf("%s is required", lookupField),
		})
	}

	objectID, err := normalizeObjectID(objectIDRaw)
	if err != nil {
		errors = append(errors, ValidationError{
			Field:   lookupField,
			Code:    "invalid_value",
			Message: err.Error(),
		})
	}
	if objectID != "" {
		normalized[lookupField] = objectID
	}

	if mode == ModeCreate {
		for fieldName, field := range table.Fields {
			if field.Archived || isManagedField(model.ManagedSystemFields, fieldName) {
				continue
			}
			if fieldName == lookupField {
				continue
			}
			if _, exists := normalized[fieldName]; exists {
				continue
			}
			if !field.Nullable {
				errors = append(errors, ValidationError{
					Field:   fieldName,
					Code:    "missing_required",
					Message: fmt.Sprintf("field %s is required for full writes", fieldName),
				})
				continue
			}
			normalized[fieldName] = nil
		}
	}

	if len(errors) > 0 {
		return nil, objectID, errors
	}

	return normalized, objectID, nil
}

func MarshalPayload(payload map[string]any) []byte {
	body, _ := json.Marshal(payload)
	return body
}

func MarshalValidationErrors(errors []ValidationError) []byte {
	body, _ := json.Marshal(errors)
	return body
}

func isManagedField(fields []string, candidate string) bool {
	for _, field := range fields {
		if field == candidate {
			return true
		}
	}
	return false
}

func normalizeObjectID(value any) (string, error) {
	raw, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("object_id must be a string")
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("object_id must not be empty")
	}
	return raw, nil
}

func normalizeFieldValue(field FieldSchema, value any, mode Mode) (any, error) {
	if value == nil {
		if !field.Nullable {
			return nil, fmt.Errorf("field %s does not allow null values", field.Name)
		}
		return nil, nil
	}

	switch field.DataType {
	case "bool":
		switch typed := value.(type) {
		case bool:
			return typed, nil
		case string:
			trimmed := strings.TrimSpace(strings.ToLower(typed))
			if trimmed == "true" {
				return true, nil
			}
			if trimmed == "false" {
				return false, nil
			}
		}
		return nil, fmt.Errorf("field %s must be a boolean", field.Name)
	case "int":
		normalized, err := normalizeInteger(value)
		if err != nil {
			return nil, fmt.Errorf("field %s must be an integer", field.Name)
		}
		return normalized, validateEnumValue(field, normalized)
	case "float":
		normalized, err := normalizeFloat(value)
		if err != nil {
			return nil, fmt.Errorf("field %s must be a float", field.Name)
		}
		return normalized, validateEnumValue(field, normalized)
	case "timestamp":
		stringValue, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("field %s must be an RFC3339 timestamp string", field.Name)
		}
		parsed, err := time.Parse(time.RFC3339, stringValue)
		if err != nil {
			parsed, err = time.Parse(time.RFC3339Nano, stringValue)
			if err != nil {
				return nil, fmt.Errorf("field %s must be an RFC3339 timestamp string", field.Name)
			}
		}
		return parsed.UTC(), nil
	case "ip_address":
		stringValue, ok := value.(string)
		if !ok || net.ParseIP(strings.TrimSpace(stringValue)) == nil {
			return nil, fmt.Errorf("field %s must be a valid IP address", field.Name)
		}
		return strings.TrimSpace(stringValue), nil
	case "string":
		stringValue, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("field %s must be a string", field.Name)
		}
		if field.IsEnum {
			if err := validateEnumValue(field, stringValue); err != nil {
				return nil, err
			}
		}
		return stringValue, nil
	default:
		return nil, fmt.Errorf("field %s has unsupported data type %s", field.Name, field.DataType)
	}
}

func validateEnumValue(field FieldSchema, value any) error {
	if !field.IsEnum {
		return nil
	}
	stringValue := fmt.Sprintf("%v", value)
	for _, enumValue := range field.EnumValues {
		if enumValue.Value == stringValue {
			return nil
		}
	}
	return fmt.Errorf("field %s value %v is not in the managed enum catalog", field.Name, value)
}

func normalizeInteger(value any) (int64, error) {
	switch typed := value.(type) {
	case float64:
		if typed != float64(int64(typed)) {
			return 0, fmt.Errorf("not an integer")
		}
		return int64(typed), nil
	case int:
		return int64(typed), nil
	case int64:
		return typed, nil
	case json.Number:
		return typed.Int64()
	case string:
		return strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
	default:
		return 0, fmt.Errorf("not an integer")
	}
}

func normalizeFloat(value any) (float64, error) {
	switch typed := value.(type) {
	case float64:
		return typed, nil
	case float32:
		return float64(typed), nil
	case int:
		return float64(typed), nil
	case int64:
		return float64(typed), nil
	case json.Number:
		return typed.Float64()
	case string:
		return strconv.ParseFloat(strings.TrimSpace(typed), 64)
	default:
		return 0, fmt.Errorf("not a float")
	}
}
