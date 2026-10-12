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
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Enabled    bool       `json:"enabled"`
	Outcomes   []string   `json:"outcomes"`
	ActionType ActionType `json:"action_type"`
	InboxID    string     `json:"inbox_id"`
	Title      string     `json:"title"`
	TagIDs     []string   `json:"tag_ids"`
	UpdatedAt  *time.Time `json:"updated_at"`
}

func (r CaseRule) Validate() error {
	for _, id := range append([]string{r.ID, r.InboxID}, r.TagIDs...) {
		if v, e := uuid.Parse(id); e != nil || v == uuid.Nil {
			return fmt.Errorf("invalid resource ID")
		}
	}
	if strings.TrimSpace(r.Name) == "" || len(r.Name) > 200 || strings.TrimSpace(r.Title) == "" || len(r.Title) > 200 {
		return fmt.Errorf("rule name and literal case title are required (maximum 200 bytes)")
	}
	if r.ActionType != ActionTypeCreateCase && r.ActionType != ActionTypeAddToCaseIfPossible {
		return fmt.Errorf("unsupported case action")
	}
	if len(r.Outcomes) == 0 || len(r.Outcomes) > 4 || len(r.TagIDs) > 25 {
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
