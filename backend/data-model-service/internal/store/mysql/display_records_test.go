package mysql

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/display"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestDisplayRecordQueryUsesExactKeysAndBoundedProjection(t *testing.T) {
	tenant, table, parent, object, child, target, caption, link := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	s := DisplayStore{schema: "`fraud`", tenant: tenant}
	settings := display.Settings{TableID: table, Fields: []display.Field{{ID: object, Name: "object_id", Type: "string"}, {ID: child, Name: "ref", Type: "string"}}, Links: []display.Link{{ID: link, FieldID: child, Table: "parent", Caption: "caption", TargetFields: []display.Field{{ID: target, Name: "key", Type: "string"}, {ID: caption, Name: "caption", Type: "string"}}}}, DisplayColumns: []display.DisplayColumn{{ID: "rel", Type: "relation", LinkID: &link, TargetFieldID: &caption}}}
	projection, e := display.ResolveProjection(settings)
	if e != nil {
		t.Fatal(e)
	}
	mapping := map[uuid.UUID]displayPhysical{table: {name: "`fraud`.`child`", columns: map[uuid.UUID]map[string]string{object: {"value": "`object`"}, child: {"value": "`ref`"}}}, parent: {name: "`fraud`.`parent`", columns: map[uuid.UUID]map[string]string{target: {"value": "`key`"}, caption: {"value": "`caption`"}}}}
	q, args, e := s.displayRecordQuery(settings, map[uuid.UUID]displayLink{link: {parent: parent, parentField: target}}, projection, []string{"a'unsafe", "b", "b"}, mapping, 1024)
	if e != nil {
		t.Fatal(e)
	}
	for _, part := range []string{"record_object_key_id=requested.key_id", "bucket_hash=", "raw_value=CAST(r.`ref` AS BINARY)", "record_text_key_", "valid_until IS NULL", "LIMIT 3", "<=1024"} {
		if !strings.Contains(q, part) {
			t.Fatal("missing indexed/bounded clause", part, q)
		}
	}
	if strings.Contains(q, "unsafe") || len(args) != 6 {
		t.Fatal("unbound value or duplicate lookup", args)
	}
	if v, e := displayScalar([]byte("1"), "bool"); e != nil || v != true {
		t.Fatal(v, e)
	}
	if v, e := displayScalar([]byte("1.25"), "float"); e != nil || v != 1.25 {
		t.Fatal(v, e)
	}
	if _, e := displayScalar([]byte("bad"), "int"); e == nil {
		t.Fatal("invalid integer accepted")
	}
}
