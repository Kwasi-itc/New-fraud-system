package workflow

import "encoding/json"

// DecodeCaseRule is the portable browser projection shared by storage adapters.
// It recognizes only the single-condition/single-action v1/v2 authoring formats;
// arbitrary structured workflows are not silently rewritten as browser rules.
func DecodeCaseRule(v StructuredRule) (CaseRule, bool) {
	var out CaseRule
	if v.Rule.Fallthrough || len(v.Conditions) != 1 || len(v.Actions) != 1 {
		return out, false
	}
	c, a := v.Conditions[0], v.Actions[0]
	var cfg struct {
		Version   int      `json:"authoring_version"`
		Inbox     string   `json:"inbox_id"`
		Name      string   `json:"name"`
		TitleMode string   `json:"title_mode"`
		Tags      []string `json:"tag_ids"`
	}
	if json.Unmarshal(a.ActionConfig, &cfg) != nil || (cfg.Version != 1 && cfg.Version != 2) {
		return out, false
	}
	if cfg.Version == 1 {
		if c.Function != ConditionNever && c.Function != ConditionOutcomeIn {
			return out, false
		}
		if json.Unmarshal(c.Params, &out.Outcomes) != nil {
			return out, false
		}
	} else {
		out.Outcomes = []string{}
		if c.Function != ConditionNever && c.Function != ConditionTreeMatch {
			return out, false
		}
		if json.Unmarshal(c.Params, &out.Condition) != nil || out.Condition == nil {
			return out, false
		}
	}
	out.ID, out.Name, out.Enabled = v.Rule.ID, v.Rule.Name, c.Function != ConditionNever
	out.ActionType, out.InboxID, out.Title, out.TitleMode = a.ActionType, cfg.Inbox, cfg.Name, cfg.TitleMode
	out.TagIDs = cfg.Tags
	if out.TagIDs == nil {
		out.TagIDs = []string{}
	}
	out.UpdatedAt = &v.Rule.UpdatedAt
	return out, out.Validate() == nil
}
