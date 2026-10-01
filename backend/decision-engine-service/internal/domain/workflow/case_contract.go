package workflow

import (
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/google/uuid"
)

func IsCaseAction(kind ActionType) bool {
	return kind == ActionTypeCreateCase || kind == ActionTypeAddToCase || kind == ActionTypeAddToCaseIfPossible
}

func ValidateActionConfig(kind ActionType, raw json.RawMessage) error {
	if !IsCaseAction(kind) && kind != ActionTypeAddTag && kind != ActionTypeEmitEvent {
		return fmt.Errorf("unsupported action_type %q", kind)
	}
	var cfg struct {
		InboxID       string          `json:"inbox_id"`
		TagIDs        []string        `json:"tag_ids"`
		URL           string          `json:"url"`
		TitleTemplate json.RawMessage `json:"title_template"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("action_config must be a JSON object: %w", err)
	}
	if IsCaseAction(kind) {
		if id, err := uuid.Parse(cfg.InboxID); err != nil || id == uuid.Nil {
			return fmt.Errorf("case action requires a valid inbox_id")
		}
		for _, rawID := range cfg.TagIDs {
			if id, err := uuid.Parse(rawID); err != nil || id == uuid.Nil {
				return fmt.Errorf("case tag_ids must be UUIDs")
			}
		}
		if cfg.URL != "" {
			return fmt.Errorf("case actions use the configured case service, not a per-action URL")
		}
		if len(cfg.TitleTemplate) > 0 {
			return fmt.Errorf("title_template is not supported; use a literal name or title")
		}
		return nil
	}
	// Other action types require an explicit dispatcher, never case intake.
	u, err := url.Parse(cfg.URL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return fmt.Errorf("non-case action requires an explicit HTTP(S) url")
	}
	return nil
}
