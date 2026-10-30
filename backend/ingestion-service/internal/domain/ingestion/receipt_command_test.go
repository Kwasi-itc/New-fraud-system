package ingestion

import (
	"crypto/sha256"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func testReceiptCommand() ReceiptCommand {
	return ReceiptCommand{TenantID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), TableID: uuid.MustParse("22222222-2222-2222-2222-222222222222"), Mode: ModeCreate, Kind: "single", Records: []map[string]any{{"z": nil, "a": "x", "number": json.Number("9007199254740993")}}}
}

func TestCanonicalReceiptCommandGolden(t *testing.T) {
	c := testReceiptCommand()
	want := `{"version":"ingestion-request/v1","tenant":"11111111-1111-1111-1111-111111111111","table":"22222222-2222-2222-2222-222222222222","mode":"create","kind":"single","records":[{"a":"x","number":9007199254740993,"z":null}]}`
	wire, hash, err := CanonicalReceiptCommand(c, len(want))
	if err != nil || string(wire) != want || hash != sha256.Sum256([]byte(want)) {
		t.Fatal(string(wire), hash, err)
	}
	if wire, hash, err := CanonicalReceiptCommand(c, len(want)-1); err == nil || wire != nil || hash != ([32]byte{}) {
		t.Fatal("limit returned identity", err)
	}
	// Map insertion order cannot change identity; the input is not modified.
	c.Records[0] = map[string]any{"number": json.Number("9007199254740993"), "z": nil, "a": "x"}
	_, again, err := CanonicalReceiptCommand(c, 1024)
	if err != nil || again != hash || c.Records[0]["number"] != json.Number("9007199254740993") {
		t.Fatal("unstable or mutated", err)
	}
}

func TestCanonicalReceiptCommandIdentity(t *testing.T) {
	base := testReceiptCommand()
	_, original, _ := CanonicalReceiptCommand(base, 4096)
	for _, name := range []string{"tenant", "table", "mode", "kind", "value", "null", "unicode", "numeric"} {
		t.Run(name, func(t *testing.T) {
			c := testReceiptCommand()
			switch name {
			case "tenant":
				c.TenantID = uuid.New()
			case "table":
				c.TableID = uuid.New()
			case "mode":
				c.Mode = ModePatch
			case "kind":
				c.Kind = "batch"
			case "value":
				c.Records[0]["a"] = "x "
			case "null":
				delete(c.Records[0], "z")
			case "unicode":
				c.Records[0]["a"] = "e\u0301"
			case "numeric":
				c.Records[0]["number"] = json.Number("9007199254740993.0")
			}
			_, changed, err := CanonicalReceiptCommand(c, 4096)
			if err != nil || changed == original {
				t.Fatal("unbound input", err)
			}
		})
	}
	c := testReceiptCommand()
	c.Kind = "batch"
	c.Records = append(c.Records, map[string]any{"a": "second"})
	_, before, _ := CanonicalReceiptCommand(c, 4096)
	c.Records[0], c.Records[1] = c.Records[1], c.Records[0]
	_, after, err := CanonicalReceiptCommand(c, 4096)
	if err != nil || before == after {
		t.Fatal("record order lost", err)
	}
}

func TestCanonicalReceiptCommandRejection(t *testing.T) {
	cycle := map[string]any{}
	cycle["cycle"] = cycle
	for _, value := range []any{math.NaN(), math.Inf(1), "\xff", json.Number(""), json.Number("01"), json.Number("NaN"), []byte("abc"), int64(1), cycle, map[string]any{"\xff": true}, json.RawMessage(`{}`)} {
		c := testReceiptCommand()
		c.Records[0]["a"] = value
		if wire, hash, err := CanonicalReceiptCommand(c, 4096); err == nil || wire != nil || hash != ([32]byte{}) {
			t.Fatalf("accepted %T", value)
		}
	}
	for _, name := range []string{"tenant", "table", "mode", "kind", "empty", "nil_record", "single_many", "batch_many", "bytes"} {
		t.Run(name, func(t *testing.T) {
			c := testReceiptCommand()
			switch name {
			case "tenant":
				c.TenantID = uuid.Nil
			case "table":
				c.TableID = uuid.Nil
			case "mode":
				c.Mode = "other"
			case "kind":
				c.Kind = "other"
			case "empty":
				c.Records = nil
			case "nil_record":
				c.Records[0] = nil
			case "single_many":
				c.Records = append(c.Records, map[string]any{})
			case "batch_many":
				c.Kind = "batch"
				c.Records = make([]map[string]any, 501)
			case "bytes":
				c.Records[0]["a"] = strings.Repeat("x", 4097)
			}
			if wire, hash, err := CanonicalReceiptCommand(c, 4096); err == nil || wire != nil || hash != ([32]byte{}) {
				t.Fatal("invalid command accepted")
			}
		})
	}
}
