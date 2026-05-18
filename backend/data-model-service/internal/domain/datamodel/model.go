package datamodel

import (
	"cmp"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

var validNameRegex = regexp.MustCompile(`^[a-z]+[a-z0-9_]+$`)

var reservedFieldNames = map[string]struct{}{
	"id":          {},
	"object_id":   {},
	"updated_at":  {},
	"valid_from":  {},
	"valid_until": {},
}

type DataType string

const (
	DataTypeBool      DataType = "bool"
	DataTypeInt       DataType = "int"
	DataTypeFloat     DataType = "float"
	DataTypeString    DataType = "string"
	DataTypeTimestamp DataType = "timestamp"
	DataTypeIPAddress DataType = "ip_address"
)

func SupportedDataTypes() []DataType {
	return []DataType{
		DataTypeBool,
		DataTypeInt,
		DataTypeFloat,
		DataTypeString,
		DataTypeTimestamp,
		DataTypeIPAddress,
	}
}

func ParseDataType(value string) (DataType, error) {
	dataType := DataType(strings.TrimSpace(value))
	for _, supported := range SupportedDataTypes() {
		if dataType == supported {
			return dataType, nil
		}
	}

	return "", fmt.Errorf("unsupported data type: %s", value)
}

type Table struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	Name         string
	Description  string
	Alias        string
	SemanticType string
	CaptionField string
	Archived     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Field struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	TableID     uuid.UUID
	Name        string
	Description string
	DataType    DataType
	Nullable    bool
	IsEnum      bool
	IsUnique    bool
	Archived    bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type FieldEnumValue struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	FieldID   uuid.UUID
	Value     string
	Label     string
	SortOrder int
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Link struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	Name        string
	ParentTable uuid.UUID
	ParentField uuid.UUID
	ChildTable  uuid.UUID
	ChildField  uuid.UUID
	CreatedAt   time.Time
}

type Pivot struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	BaseTableID uuid.UUID
	FieldID     *uuid.UUID
	PathLinkIDs []uuid.UUID
	CreatedAt   time.Time
}

type TableOptions struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	TableID         uuid.UUID
	DisplayedFields []uuid.UUID
	FieldOrder      []uuid.UUID
	UpdatedAt       time.Time
}

type NavigationOption struct {
	ID                uuid.UUID `json:"id"`
	TenantID          uuid.UUID `json:"tenant_id"`
	SourceTableID     uuid.UUID `json:"source_table_id"`
	SourceFieldID     uuid.UUID `json:"source_field_id"`
	TargetTableID     uuid.UUID `json:"target_table_id"`
	FilterFieldID     uuid.UUID `json:"filter_field_id"`
	OrderingFieldID   uuid.UUID `json:"ordering_field_id"`
	SourceTableName   string    `json:"source_table_name"`
	SourceFieldName   string    `json:"source_field_name"`
	TargetTableName   string    `json:"target_table_name"`
	FilterFieldName   string    `json:"filter_field_name"`
	OrderingFieldName string    `json:"ordering_field_name"`
	CreatedAt         time.Time `json:"created_at"`
}

type AssembledDataModel struct {
	Tables map[string]AssembledTable
	Pivots []AssembledPivot
}

type AssembledTable struct {
	ID                uuid.UUID
	Name              string
	Description       string
	Alias             string
	SemanticType      string
	CaptionField      string
	Fields            map[string]AssembledField
	LinksToSingle     map[string]AssembledLink
	NavigationOptions []NavigationOption
	Options           *TableOptions
}

type AssembledField struct {
	ID          uuid.UUID
	Name        string
	Description string
	DataType    DataType
	Nullable    bool
	IsEnum      bool
	IsUnique    bool
	EnumValues  []FieldEnumValue
}

type AssembledLink struct {
	ID              uuid.UUID
	Name            string
	ParentTableID   uuid.UUID
	ParentFieldID   uuid.UUID
	ChildTableID    uuid.UUID
	ChildFieldID    uuid.UUID
	ParentTableName string
	ParentFieldName string
	ChildTableName  string
	ChildFieldName  string
}

type AssembledPivot struct {
	ID          uuid.UUID
	BaseTableID uuid.UUID
	BaseTable   string
	FieldID     *uuid.UUID
	Field       string
	PathLinkIDs []uuid.UUID
	PathLinks   []string
}

type DeleteReport struct {
	Performed bool            `json:"performed"`
	Conflicts DeleteConflicts `json:"conflicts"`
}

type DeleteConflicts struct {
	Reserved bool        `json:"reserved"`
	Links    []uuid.UUID `json:"links"`
	Pivots   []uuid.UUID `json:"pivots"`
}

type SchemaChange struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	Operation    string
	ResourceType string
	ResourceID   uuid.UUID
	Status       string
	Details      []byte
	CreatedAt    time.Time
}

type TenantSchemaMigration struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	Version   string
	AppliedAt time.Time
}

type IndexJobType string

const (
	IndexJobTypeNavigation IndexJobType = "navigation"
	IndexJobTypeSearch     IndexJobType = "search"
	IndexJobTypeRepair     IndexJobType = "repair"
)

type IndexJobStatus string

const (
	IndexJobStatusPending   IndexJobStatus = "pending"
	IndexJobStatusRunning   IndexJobStatus = "running"
	IndexJobStatusApplied   IndexJobStatus = "applied"
	IndexJobStatusFailed    IndexJobStatus = "failed"
	IndexJobStatusCancelled IndexJobStatus = "cancelled"
)

type IndexJob struct {
	ID                   uuid.UUID
	TenantID             uuid.UUID
	TableID              *uuid.UUID
	TableName            string
	IndexType            IndexJobType
	Columns              []string
	Status               IndexJobStatus
	RequestedByOperation string
	ErrorMessage         *string
	AttemptCount         int
	RequestedAt          time.Time
	StartedAt            *time.Time
	CompletedAt          *time.Time
	ScheduledAt          *time.Time
	DedupeKey            string
}

type ManagedIndexState struct {
	Name   string
	Exists bool
}

func NewDeleteReport() DeleteReport {
	return DeleteReport{
		Conflicts: DeleteConflicts{
			Links:  []uuid.UUID{},
			Pivots: []uuid.UUID{},
		},
	}
}

func NormalizeName(value string) string {
	return strings.TrimSpace(strings.ToLower(value))
}

func ParseIndexJobType(value string) (IndexJobType, error) {
	jobType := IndexJobType(strings.TrimSpace(value))
	switch jobType {
	case IndexJobTypeNavigation, IndexJobTypeSearch, IndexJobTypeRepair:
		return jobType, nil
	default:
		return "", fmt.Errorf("unsupported index job type: %s", value)
	}
}

func ValidateIndexJobCreate(jobType IndexJobType, columns []string) error {
	switch jobType {
	case IndexJobTypeNavigation, IndexJobTypeSearch, IndexJobTypeRepair:
	default:
		return fmt.Errorf("unsupported index job type: %s", jobType)
	}

	if len(columns) == 0 {
		return fmt.Errorf("at least one column is required")
	}

	seen := make(map[string]struct{}, len(columns))
	for _, column := range columns {
		name := NormalizeName(column)
		if err := ValidateObjectName("index column", name); err != nil {
			return err
		}
		if _, ok := seen[name]; ok {
			return fmt.Errorf("duplicate index column: %s", name)
		}
		seen[name] = struct{}{}
	}

	return nil
}

func BuildIndexJobDedupeKey(tenantID, tableID uuid.UUID, indexType IndexJobType, columns []string) string {
	sum := sha1.Sum([]byte(tenantID.String() + ":" + tableID.String() + ":" + string(indexType) + ":" + strings.Join(columns, ",")))
	return hex.EncodeToString(sum[:])
}

func ValidateObjectName(kind, value string) error {
	if !validNameRegex.MatchString(NormalizeName(value)) {
		return fmt.Errorf("%s name must contain only lowercase alphanumeric characters and underscores and start with a letter", kind)
	}

	return nil
}

func ValidateFieldCreate(name string, dataType DataType, isEnum, isUnique bool) error {
	if err := ValidateObjectName("field", name); err != nil {
		return err
	}

	if isReservedFieldName(name) {
		return fmt.Errorf("field name '%s' is reserved", name)
	}

	if isEnum && !supportsEnum(dataType) {
		return fmt.Errorf("enum fields can only be string, int, or float")
	}

	if isUnique && !supportsUnique(dataType) {
		return fmt.Errorf("unique fields can only be string, int, or float")
	}

	if isEnum && isUnique {
		return fmt.Errorf("a field cannot be both enum and unique")
	}

	return nil
}

func ValidateFieldUpdate(current Field, dataType DataType, isEnum, isUnique, nullable *bool) error {
	if isEnum != nil && *isEnum && !supportsEnum(dataType) {
		return fmt.Errorf("enum fields can only be string, int, or float")
	}

	if isUnique != nil && *isUnique && !supportsUnique(dataType) {
		return fmt.Errorf("unique fields can only be string, int, or float")
	}

	targetEnum := current.IsEnum
	if isEnum != nil {
		targetEnum = *isEnum
	}
	targetUnique := current.IsUnique
	if isUnique != nil {
		targetUnique = *isUnique
	}

	if targetEnum && targetUnique {
		return fmt.Errorf("a field cannot be both enum and unique")
	}

	if (current.Name == "object_id" || current.Name == "updated_at") &&
		(isEnum != nil || isUnique != nil || nullable != nil) {
		return fmt.Errorf("only the description of object_id and updated_at can be updated")
	}

	return nil
}

func ValidateTableCreate(name string) error {
	return ValidateObjectName("table", name)
}

func ValidateLinkName(name string) error {
	return ValidateObjectName("link", name)
}

func ValidatePivot(fieldID *uuid.UUID, pathLinkIDs []uuid.UUID) error {
	hasField := fieldID != nil
	hasPath := len(pathLinkIDs) > 0
	if hasField == hasPath {
		return fmt.Errorf("either field_id or path_link_ids must be provided")
	}
	return nil
}

func ValidateEnumValueCreate(field Field, value, label string) error {
	if !field.IsEnum {
		return fmt.Errorf("field is not marked as enum")
	}
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("enum value is required")
	}
	if err := validateEnumValueForDataType(field.DataType, value); err != nil {
		return err
	}
	if strings.TrimSpace(label) == "" {
		return fmt.Errorf("enum label is required")
	}
	return nil
}

func ValidateEnumValueUpdate(field Field, value, label *string) error {
	if !field.IsEnum {
		return fmt.Errorf("field is not marked as enum")
	}
	if value != nil {
		if strings.TrimSpace(*value) == "" {
			return fmt.Errorf("enum value is required")
		}
		if err := validateEnumValueForDataType(field.DataType, *value); err != nil {
			return err
		}
	}
	if label != nil && strings.TrimSpace(*label) == "" {
		return fmt.Errorf("enum label is required")
	}
	return nil
}

func SortFieldOrder(fields []Field, current TableOptions) []uuid.UUID {
	if len(fields) == 0 {
		return []uuid.UUID{}
	}

	fieldsByID := make(map[uuid.UUID]Field, len(fields))
	for _, field := range fields {
		fieldsByID[field.ID] = field
	}

	ordered := make([]uuid.UUID, 0, len(fields))
	for _, fieldID := range current.FieldOrder {
		if _, ok := fieldsByID[fieldID]; ok {
			ordered = append(ordered, fieldID)
			delete(fieldsByID, fieldID)
		}
	}

	remaining := slices.Collect(maps.Values(fieldsByID))
	slices.SortFunc(remaining, func(lhs, rhs Field) int {
		return cmp.Compare(lhs.Name, rhs.Name)
	})
	for _, field := range remaining {
		ordered = append(ordered, field.ID)
	}

	return ordered
}

func supportsUnique(dataType DataType) bool {
	return dataType == DataTypeString || dataType == DataTypeInt || dataType == DataTypeFloat
}

func supportsEnum(dataType DataType) bool {
	return supportsUnique(dataType)
}

func isReservedFieldName(name string) bool {
	_, ok := reservedFieldNames[NormalizeName(name)]
	return ok
}

func validateEnumValueForDataType(dataType DataType, value string) error {
	trimmed := strings.TrimSpace(value)
	switch dataType {
	case DataTypeString:
		return nil
	case DataTypeInt:
		if _, err := strconv.Atoi(trimmed); err != nil {
			return fmt.Errorf("enum value must be a valid int")
		}
		return nil
	case DataTypeFloat:
		if _, err := strconv.ParseFloat(trimmed, 64); err != nil {
			return fmt.Errorf("enum value must be a valid float")
		}
		return nil
	default:
		return fmt.Errorf("enum values are only supported for string, int, or float fields")
	}
}
