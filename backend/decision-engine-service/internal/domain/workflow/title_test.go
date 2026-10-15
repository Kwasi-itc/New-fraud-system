package workflow

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTitlePlaceholders(t *testing.T) {
	values := map[string]string{"object_id": "TX-%score%", "object_type": "transaction", "decision_id": "d1", "scenario_id": "s1", "outcome": "review", "score": "75"}
	got, err := ExpandTitle("%object_type% %object_id% | %decision_id% | %scenario_id% | %outcome% %score% (100%%)", values)
	if err != nil || got != "transaction TX-%score% | d1 | s1 | review 75 (100%)" {
		t.Fatal(got, err)
	}
	for _, value := range []string{"%unknown%", "%object_id", "100%"} {
		if _, err := ExpandTitle(value, values); err == nil {
			t.Fatal("invalid template accepted", value)
		}
	}
	got, err = ExpandTitle("Case %object_id%", map[string]string{"object_id": strings.Repeat("é", 200)})
	if err != nil || len(got) > 200 || !utf8.ValidString(got) || !strings.HasSuffix(got, "…") {
		t.Fatal("invalid truncation", got, err)
	}
}
