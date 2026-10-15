package service

import (
	"encoding/json"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"strconv"
)

// Resolve before enqueueing so retries retain the exact title from the decision.
func workflowActionConfig(kind workflow.ActionType, raw json.RawMessage, item decision.Decision) (json.RawMessage, error) {
	if !workflow.IsCaseAction(kind) || len(raw) == 0 {
		return raw, nil
	}
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	var mode string
	if value := cfg["title_mode"]; len(value) > 0 {
		if err := json.Unmarshal(value, &mode); err != nil {
			return nil, err
		}
	}
	if mode == "" {
		return raw, nil
	}
	if mode != "template" {
		return nil, fmt.Errorf("invalid workflow title mode")
	}
	var template string
	if err := json.Unmarshal(cfg["name"], &template); err != nil {
		return nil, err
	}
	title, err := workflow.ExpandTitle(template, map[string]string{"object_id": item.ObjectID, "object_type": item.ObjectType, "decision_id": item.ID, "scenario_id": item.ScenarioID, "outcome": string(item.Outcome), "score": strconv.Itoa(item.Score)})
	if err != nil {
		return nil, err
	}
	if title == "" {
		return nil, fmt.Errorf("workflow title resolves to an empty value")
	}
	cfg["name"], err = json.Marshal(title)
	if err != nil {
		return nil, err
	}
	delete(cfg, "title_mode")
	return json.Marshal(cfg)
}
