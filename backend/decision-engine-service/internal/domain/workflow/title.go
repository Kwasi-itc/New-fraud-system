package workflow

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

var titleKeys = map[string]bool{"object_id": true, "object_type": true, "decision_id": true, "scenario_id": true, "outcome": true, "score": true}

// ExpandTitle uses only explicit decision metadata, never arbitrary expressions.
// %% escapes a percent sign. Values are substituted once, not recursively.
func ExpandTitle(template string, values map[string]string) (string, error) {
	var out strings.Builder
	for len(template) > 0 {
		i := strings.IndexByte(template, '%')
		if i < 0 {
			out.WriteString(template)
			break
		}
		out.WriteString(template[:i])
		template = template[i+1:]
		j := strings.IndexByte(template, '%')
		if j < 0 {
			return "", fmt.Errorf("unclosed title placeholder; use %%%% for a literal percent sign")
		}
		key := template[:j]
		template = template[j+1:]
		if key == "" {
			out.WriteByte('%')
			continue
		}
		if !titleKeys[key] {
			return "", fmt.Errorf("unknown title placeholder %%%s%%", key)
		}
		if values != nil {
			out.WriteString(values[key])
		}
	}
	result := strings.TrimSpace(out.String())
	// Case titles have a fixed bound. Keep valid UTF-8 when metadata is lengthy.
	if len(result) > 200 {
		result = result[:197]
		for !utf8.ValidString(result) {
			result = result[:len(result)-1]
		}
		result += "…"
	}
	return result, nil
}
