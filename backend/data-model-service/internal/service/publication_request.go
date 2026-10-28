package service

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/google/uuid"
)

// PublicationRequestContext must come from verified identity and owner-resolved
// scope, not client-supplied actor/tenant headers. Hashing is not authorization.
// Session IDs and authz versions are intentionally excluded: refreshed sessions
// may retry the same logical request, after current authorization succeeds.
type PublicationRequestContext struct {
	TenantID  uuid.UUID
	TableID   uuid.UUID
	ActorID   uuid.UUID
	Condition contract.PublicationCondition
}

// PublicationRequestHash defines the dm-publication-request/v1 format for typed
// owner inputs. No generic map/raw-JSON fallback is allowed. Add new operations
// explicitly; never change this encoding for a supported operation during its
// receipt retention window. Operation IDs are receipt keys, not request content.
// Existing inputs must be passed unchanged to the eventual owner mutation.
func PublicationRequestHash(scope PublicationRequestContext, input any) ([32]byte, error) {
	payload, err := encodePublicationRequest(scope, input)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(payload), nil
}

type enumSeedRequest struct {
	Value     string `json:"value"`
	Label     string `json:"label"`
	SortOrder int    `json:"sort_order"`
}

func encodePublicationRequest(scope PublicationRequestContext, input any) ([]byte, error) {
	if scope.TenantID == uuid.Nil || scope.TableID == uuid.Nil || scope.ActorID == uuid.Nil {
		return nil, errors.New("verified tenant, table and actor identities required")
	}
	if err := scope.Condition.Validate(); err != nil {
		return nil, err
	}
	var kind string
	var body any
	var texts []string
	addOptional := func(values ...*string) {
		for _, v := range values {
			if v != nil {
				texts = append(texts, *v)
			}
		}
	}
	switch in := input.(type) {
	case CreateNavigationOptionInput:
		if in.TenantID != scope.TenantID || in.SourceTableID != scope.TableID || in.SourceFieldID == uuid.Nil || in.TargetTableID == uuid.Nil || in.FilterFieldID == uuid.Nil || in.OrderingFieldID == uuid.Nil {
			return nil, errors.New("navigation create requires scoped source and complete endpoints")
		}
		kind = "navigation.create"
		body = struct {
			TenantID        uuid.UUID `json:"tenant_id"`
			SourceTableID   uuid.UUID `json:"source_table_id"`
			SourceFieldID   uuid.UUID `json:"source_field_id"`
			TargetTableID   uuid.UUID `json:"target_table_id"`
			FilterFieldID   uuid.UUID `json:"filter_field_id"`
			OrderingFieldID uuid.UUID `json:"ordering_field_id"`
		}{in.TenantID, in.SourceTableID, in.SourceFieldID, in.TargetTableID, in.FilterFieldID, in.OrderingFieldID}
	case CreateTableInput:
		if in.TenantID != scope.TenantID {
			return nil, errors.New("table create tenant scope mismatch")
		}
		kind = "table.create"
		texts = append(texts, in.Name, in.Description, in.Alias, in.SemanticType)
		// The owner must allocate scope.TableID once and retain it across retries;
		// this helper neither allocates identities nor authorizes registration.
		body = struct {
			TenantID     uuid.UUID `json:"tenant_id"`
			Name         string    `json:"name"`
			Description  string    `json:"description"`
			Alias        string    `json:"alias"`
			SemanticType string    `json:"semantic_type"`
		}{in.TenantID, in.Name, in.Description, in.Alias, in.SemanticType}
	case CreateLinkInput:
		if in.TenantID != scope.TenantID || in.ChildTable != scope.TableID || in.ParentTable == uuid.Nil || in.ParentField == uuid.Nil || in.ChildField == uuid.Nil {
			return nil, errors.New("link create requires scoped child and complete endpoints")
		}
		kind = "link.create"
		texts = append(texts, in.Name)
		body = struct {
			TenantID    uuid.UUID `json:"tenant_id"`
			Name        string    `json:"name"`
			ParentTable uuid.UUID `json:"parent_table_id"`
			ParentField uuid.UUID `json:"parent_field_id"`
			ChildTable  uuid.UUID `json:"child_table_id"`
			ChildField  uuid.UUID `json:"child_field_id"`
		}{in.TenantID, in.Name, in.ParentTable, in.ParentField, in.ChildTable, in.ChildField}
	case CreatePivotInput:
		if in.TenantID != scope.TenantID || in.BaseTableID != scope.TableID {
			return nil, errors.New("pivot create scope mismatch")
		}
		kind = "pivot.create"
		body = struct {
			TenantID    uuid.UUID   `json:"tenant_id"`
			BaseTableID uuid.UUID   `json:"base_table_id"`
			FieldID     *uuid.UUID  `json:"field_id"`
			PathLinkIDs []uuid.UUID `json:"path_link_ids"`
		}{in.TenantID, in.BaseTableID, in.FieldID, in.PathLinkIDs}
	case CreateIndexJobInput:
		if in.TenantID != scope.TenantID || in.TableID != scope.TableID {
			return nil, errors.New("index job create scope mismatch")
		}
		kind = "index_job.create"
		texts = append(texts, string(in.IndexType), in.RequestedByOperation)
		texts = append(texts, in.Columns...)
		var scheduled *time.Time
		if in.ScheduledAt != nil {
			utc := in.ScheduledAt.UTC()
			scheduled = &utc
		}
		body = struct {
			TenantID             uuid.UUID  `json:"tenant_id"`
			TableID              uuid.UUID  `json:"table_id"`
			IndexType            string     `json:"index_type"`
			Columns              []string   `json:"columns"`
			RequestedByOperation string     `json:"requested_by_operation"`
			ScheduledAt          *time.Time `json:"scheduled_at"`
		}{in.TenantID, in.TableID, string(in.IndexType), in.Columns, in.RequestedByOperation, scheduled}
	case UpdateTableInput:
		if in.TableID != scope.TableID {
			return nil, errors.New("table update scope mismatch")
		}
		kind = "table.update"
		addOptional(in.Description, in.Alias, in.SemanticType, in.CaptionField)
		body = struct {
			TableID      uuid.UUID `json:"table_id"`
			Description  *string   `json:"description"`
			Alias        *string   `json:"alias"`
			SemanticType *string   `json:"semantic_type"`
			CaptionField *string   `json:"caption_field"`
		}{in.TableID, in.Description, in.Alias, in.SemanticType, in.CaptionField}
	case CreateFieldInput:
		if in.TableID != scope.TableID {
			return nil, errors.New("field create scope mismatch")
		}
		kind = "field.create"
		texts = append(texts, in.Name, in.Description, in.Alias, string(in.DataType))
		values := make([]enumSeedRequest, len(in.EnumValues))
		for i, value := range in.EnumValues {
			values[i] = enumSeedRequest{value.Value, value.Label, value.SortOrder}
			texts = append(texts, value.Value, value.Label)
		}
		body = struct {
			TableID     uuid.UUID         `json:"table_id"`
			Name        string            `json:"name"`
			Description string            `json:"description"`
			Alias       string            `json:"alias"`
			DataType    string            `json:"data_type"`
			Nullable    bool              `json:"nullable"`
			IsEnum      bool              `json:"is_enum"`
			IsUnique    bool              `json:"is_unique"`
			EnumValues  []enumSeedRequest `json:"enum_values"`
		}{in.TableID, in.Name, in.Description, in.Alias, string(in.DataType), in.Nullable, in.IsEnum, in.IsUnique, values}
	case UpdateFieldInput:
		if in.FieldID == uuid.Nil {
			return nil, errors.New("field identity required")
		}
		kind = "field.update"
		addOptional(in.Description, in.Alias)
		body = struct {
			FieldID     uuid.UUID `json:"field_id"`
			Description *string   `json:"description"`
			Alias       *string   `json:"alias"`
			Nullable    *bool     `json:"nullable"`
			IsEnum      *bool     `json:"is_enum"`
			IsUnique    *bool     `json:"is_unique"`
		}{in.FieldID, in.Description, in.Alias, in.Nullable, in.IsEnum, in.IsUnique}
	case CreateFieldEnumValueInput:
		if in.FieldID == uuid.Nil {
			return nil, errors.New("field identity required")
		}
		kind = "enum.create"
		texts = append(texts, in.Value, in.Label)
		body = struct {
			FieldID   uuid.UUID `json:"field_id"`
			Value     string    `json:"value"`
			Label     string    `json:"label"`
			SortOrder int       `json:"sort_order"`
		}{in.FieldID, in.Value, in.Label, in.SortOrder}
	case UpdateFieldEnumValueInput:
		if in.EnumValueID == uuid.Nil {
			return nil, errors.New("enum identity required")
		}
		kind = "enum.update"
		addOptional(in.Value, in.Label)
		body = struct {
			EnumValueID uuid.UUID `json:"enum_value_id"`
			Value       *string   `json:"value"`
			Label       *string   `json:"label"`
			SortOrder   *int      `json:"sort_order"`
		}{in.EnumValueID, in.Value, in.Label, in.SortOrder}
	default:
		return nil, errors.New("unsupported publication operation input")
	}
	for _, value := range texts {
		if !utf8.ValidString(value) {
			return nil, errors.New("publication request contains invalid UTF-8")
		}
	}
	return json.Marshal(struct {
		Format    string                        `json:"format"`
		TenantID  uuid.UUID                     `json:"tenant_id"`
		TableID   uuid.UUID                     `json:"table_id"`
		ActorID   uuid.UUID                     `json:"actor_id"`
		Condition contract.PublicationCondition `json:"condition"`
		Kind      string                        `json:"kind"`
		Body      any                           `json:"body"`
	}{"dm-publication-request/v1", scope.TenantID, scope.TableID, scope.ActorID, scope.Condition, kind, body})
}
