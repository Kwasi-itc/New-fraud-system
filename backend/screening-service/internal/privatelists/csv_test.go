package privatelists

import (
	"strings"
	"testing"
)

func TestCSVSnapshotValidation(t *testing.T) {
	good := "record_id,name,entity_type,source_reference\n001,Fictional Person,Person,own test\n"
	records, e := ParseCSV("\uFEFF" + good)
	if e != nil || len(records) != 1 || records[0].ID != "001" {
		t.Fatal(records, e)
	}
	for name, input := range map[string]string{"empty": "record_id,name,entity_type,source_reference\n", "duplicate": good + "001,Other,Person,own test\n", "unknown": strings.Replace(good, "name,", "url,", 1), "badtype": strings.Replace(good, ",Person,", ",Object,", 1), "badutf8": good + string([]byte{255}), "nul": good + "\x00", "badquote": good + "\"unterminated", "duplicatecolumn": strings.Replace(good, "name,", "record_id,", 1)} {
		t.Run(name, func(t *testing.T) {
			if _, e := ParseCSV(input); e == nil {
				t.Fatal("invalid snapshot accepted")
			}
		})
	}
}
