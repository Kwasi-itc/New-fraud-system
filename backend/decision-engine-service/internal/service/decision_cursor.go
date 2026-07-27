package service

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type decisionCursorPayload struct {
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
}

func EncodeDecisionCursor(createdAt time.Time, id string) string {
	payload, _ := json.Marshal(decisionCursorPayload{
		CreatedAt: createdAt.UTC(),
		ID:        id,
	})
	return base64.RawURLEncoding.EncodeToString(payload)
}

func DecodeDecisionCursor(raw string) (*ports.DecisionListCursor, error) {
	if raw == "" {
		return nil, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("decode cursor: %w", err)
	}
	var item decisionCursorPayload
	if err := json.Unmarshal(payload, &item); err != nil {
		return nil, fmt.Errorf("unmarshal cursor: %w", err)
	}
	if item.ID == "" || item.CreatedAt.IsZero() {
		return nil, fmt.Errorf("cursor is missing required fields")
	}
	return &ports.DecisionListCursor{
		CreatedAt: item.CreatedAt.UTC(),
		ID:        item.ID,
	}, nil
}
