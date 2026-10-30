package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/google/uuid"
)

func TestCanonicalReceiptReplay(t *testing.T) {
	db, name := receiptFixture(t)
	ctx := context.Background()
	tenant := uuid.New()
	m, err := NewReceiptManager(db, name, tenant, ReceiptLimits{128, 1024, 8}, func(context.Context, ReceiptRequest) error { return nil }, func(context.Context, *sql.Tx, ReceiptRequest) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	c := ingestion.ReceiptCommand{TenantID: tenant, TableID: uuid.New(), Mode: ingestion.ModeCreate, Kind: "single", Records: []map[string]any{{"object_id": "one", "value": json.Number("9007199254740993")}}}
	r, err := NewReceiptRequest(c, "key", 4096)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	effect := func(context.Context, *sql.Tx) (json.RawMessage, error) {
		calls++
		return json.RawMessage(`{"action":"created"}`), nil
	}
	if _, err := m.Execute(ctx, r, effect); err != nil {
		t.Fatal(err)
	}
	// A reconstructed request uses only immutable input/scope, not current model.
	c.Records[0] = map[string]any{"value": json.Number("9007199254740993"), "object_id": "one"}
	r, err = NewReceiptRequest(c, "key", 4096)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := m.Execute(ctx, r, effect); err != nil || !v.Replayed || calls != 1 {
		t.Fatal(v, err, calls)
	}
	for _, field := range []string{"table", "mode", "kind", "content"} {
		changed := c
		switch field {
		case "table":
			changed.TableID = uuid.New()
		case "mode":
			changed.Mode = ingestion.ModePatch
		case "kind":
			changed.Kind = "batch"
		case "content":
			changed.Records = []map[string]any{{"object_id": "one", "value": json.Number("9007199254740992")}}
		}
		r, err := NewReceiptRequest(changed, "key", 4096)
		if err != nil {
			t.Fatal(err)
		}
		if v, err := m.Execute(ctx, r, effect); !errors.Is(err, ErrReceiptConflict) || len(v.Payload) != 0 || calls != 1 {
			t.Fatal(field, v, err, calls)
		}
	}
}
