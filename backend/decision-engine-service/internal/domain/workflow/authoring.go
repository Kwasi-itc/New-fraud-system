package workflow

import (
	"errors"
	"fmt"
	"github.com/google/uuid"
	"slices"
	"strings"
	"time"
)

var ErrAuthoringDenied = errors.New("workflow access denied")
var ErrAuthoringConflict = errors.New("workflow changed or uses unsupported configuration; refresh before saving")

// Actor is supplied only by the authenticated, private browser gateway.
type AuthoringActor struct {
	UserID, SessionID string
	Version           int64
}
type CaseRule struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Enabled    bool           `json:"enabled"`
	Outcomes   []string       `json:"outcomes"`
	Condition  *ConditionTree `json:"condition,omitempty"`
	ActionType ActionType     `json:"action_type"`
	InboxID    string         `json:"inbox_id"`
	Title      string         `json:"title"`
	TitleMode  string         `json:"title_mode,omitempty"`
	TagIDs     []string       `json:"tag_ids"`
	UpdatedAt  *time.Time     `json:"updated_at"`
}

func (r CaseRule) Validate() error {
	if r.TitleMode != "" && r.TitleMode != "template" {
		return fmt.Errorf("invalid title mode")
	}
	if r.TitleMode == "template" {
		if r.ActionType == ActionTypeDoNothing {
			return fmt.Errorf("do nothing cannot have a title template")
		}
		if _, err := ExpandTitle(r.Title, nil); err != nil {
			return err
		}
	}
	ids := append([]string{r.ID}, r.TagIDs...)
	if r.ActionType != ActionTypeDoNothing {
		ids = append(ids, r.InboxID)
	}
	for _, id := range ids {
		if v, e := uuid.Parse(id); e != nil || v == uuid.Nil {
			return fmt.Errorf("invalid resource ID")
		}
	}
	if strings.TrimSpace(r.Name) == "" || len(r.Name) > 200 || (r.ActionType != ActionTypeDoNothing && (strings.TrimSpace(r.Title) == "" || len(r.Title) > 200)) {
		return fmt.Errorf("rule name and literal case title are required (maximum 200 bytes)")
	}
	if r.ActionType != ActionTypeCreateCase && r.ActionType != ActionTypeAddToCaseIfPossible && r.ActionType != ActionTypeDoNothing {
		return fmt.Errorf("unsupported case action")
	}
	if r.ActionType == ActionTypeDoNothing && (r.Condition == nil || r.InboxID != "" || r.Title != "" || len(r.TagIDs) != 0) {
		return fmt.Errorf("do nothing requires conditions and no case configuration")
	}
	if r.Condition != nil {
		if err := r.Condition.Validate(); err != nil {
			return err
		}
		if len(r.Outcomes) != 0 {
			return fmt.Errorf("use either condition or outcomes")
		}
	}
	if (r.Condition == nil && len(r.Outcomes) == 0) || len(r.Outcomes) > 4 || len(r.TagIDs) > 25 {
		return fmt.Errorf("select outcomes and at most 25 tags")
	}
	seen := map[string]bool{}
	for _, v := range r.Outcomes {
		if !slices.Contains([]string{"approve", "review", "block_and_review", "decline"}, v) || seen[v] {
			return fmt.Errorf("invalid or duplicate outcome")
		}
		seen[v] = true
	}
	seen = map[string]bool{}
	for _, v := range r.TagIDs {
		if seen[v] {
			return fmt.Errorf("duplicate tag")
		}
		seen[v] = true
	}
	return nil
}

type AuthoringPage struct {
	ScenarioName    string     `json:"scenario_name"`
	Rules           []CaseRule `json:"rules"`
	OtherRules      int        `json:"other_rules"`
	LegacyWorkflows bool       `json:"legacy_workflows"`
}
