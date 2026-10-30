package mysql

import (
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"testing"
)

func TestProcessEventContract(t *testing.T) {
	_, f, cfg := processFixture(t)
	api := startProcess(t, cfg, false)
	expectHTTP(t, api, "POST", recordPath(f), "create-event", `{"object_id":"one","number":1}`, 200)
	expectHTTP(t, api, "PATCH", recordPath(f), "update-event", `{"object_id":"one","amount":2}`, 200)
	expectHTTP(t, api, "POST", recordPath(f)+"/batch", "batch-event", `[{"object_id":"two","number":2}]`, 200)
	read := func() map[string]string {
		rows, e := f.db.Query("SELECT HEX(id),event_type,payload,status FROM ingestion_outbox_events WHERE tenant_id=? ORDER BY id", mysqlstore.UUID(f.model.TenantID))
		if e != nil {
			t.Fatal(e)
		}
		defer rows.Close()
		out := map[string]string{}
		types := map[string]int{}
		for rows.Next() {
			var id, kind, payload, status string
			if e = rows.Scan(&id, &kind, &payload, &status); e != nil {
				t.Fatal(e)
			}
			if status != "pending" {
				t.Fatal(status)
			}
			var body map[string]any
			if e = json.Unmarshal([]byte(payload), &body); e != nil {
				t.Fatal(e)
			}
			if body["tenant_id"] != f.model.TenantID.String() || body["object_type"] != "records" || body["ingested_at"] == nil {
				t.Fatal(body)
			}
			if kind == "batch.ingestion.completed" {
				if body["count"] != float64(1) {
					t.Fatal(body)
				}
			} else if body["object_id"] == nil || body["record"] == nil || body["action"] == nil {
				t.Fatal(body)
			}
			out[id] = payload
			types[kind]++
		}
		if e = rows.Err(); e != nil {
			t.Fatal(e)
		}
		if types["record.ingested"] != 2 || types["record.updated"] != 1 || types["batch.ingestion.completed"] != 1 {
			t.Fatal(types)
		}
		return out
	}
	before := read()
	expectHTTP(t, api, "POST", recordPath(f), "create-event", `{"object_id":"one","number":1}`, 200)
	after := read()
	if len(before) != len(after) {
		t.Fatal(before, after)
	}
	for id, payload := range before {
		if after[id] != payload {
			t.Fatal("replay changed event", id)
		}
	}
}
