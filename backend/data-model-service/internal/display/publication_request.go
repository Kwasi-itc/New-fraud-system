package display

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/google/uuid"
)

// SaveRequestHash binds the exact display command to owner-resolved scope,
// trusted actor, display version and ingestion condition. It is not authorization.
// The pinned wire types must not silently follow future changes to SaveRequest.
func SaveRequestHash(tenant, table, actor uuid.UUID, condition contract.PublicationCondition, in SaveRequest) ([32]byte, error) {
	data, err := encodeSaveRequest(tenant, table, actor, condition, in)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(data), nil
}

func encodeSaveRequest(tenant, table, actor uuid.UUID, condition contract.PublicationCondition, in SaveRequest) ([]byte, error) {
	if tenant == uuid.Nil || table == uuid.Nil || actor == uuid.Nil || in.Version == nil || *in.Version < 0 {
		return nil, errors.New("trusted tenant/table/actor and nonnegative display version required")
	}
	if err := condition.Validate(); err != nil {
		return nil, err
	}
	type field struct {
		Label  string     `json:"label"`
		LinkID *uuid.UUID `json:"link_id"`
	}
	type column struct {
		ID            string     `json:"id"`
		Type          string     `json:"type"`
		Label         string     `json:"label"`
		FieldID       *uuid.UUID `json:"field_id"`
		LinkID        *uuid.UUID `json:"link_id"`
		TargetFieldID *uuid.UUID `json:"target_field_id"`
	}
	texts := []string{in.Caption}
	var fields map[string]field
	if in.Display != nil {
		fields = make(map[string]field, len(in.Display))
	}
	for key, v := range in.Display {
		texts = append(texts, key, v.Label)
		fields[key] = field{v.Label, v.LinkID}
	}
	var columns []column
	if in.DisplayColumns != nil {
		columns = make([]column, len(in.DisplayColumns))
	}
	for i, v := range in.DisplayColumns {
		texts = append(texts, v.ID, v.Type, v.Label)
		columns[i] = column{v.ID, v.Type, v.Label, v.FieldID, v.LinkID, v.TargetFieldID}
	}
	for _, v := range texts {
		if !utf8.ValidString(v) {
			return nil, errors.New("display request contains invalid UTF-8")
		}
	}
	return json.Marshal(struct {
		Format    string                        `json:"format"`
		TenantID  uuid.UUID                     `json:"tenant_id"`
		TableID   uuid.UUID                     `json:"table_id"`
		ActorID   uuid.UUID                     `json:"actor_id"`
		Condition contract.PublicationCondition `json:"condition"`
		Version   int64                         `json:"display_version"`
		Caption   string                        `json:"caption_field"`
		Displayed []uuid.UUID                   `json:"displayed_fields"`
		Order     []uuid.UUID                   `json:"field_order"`
		Display   map[string]field              `json:"field_display"`
		Columns   []column                      `json:"display_columns"`
	}{"dm-display-save/v1", tenant, table, actor, condition, *in.Version, in.Caption, in.Displayed, in.Order, fields, columns})
}
