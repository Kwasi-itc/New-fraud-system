package contract

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/google/uuid"
)

func TestPrepareIngestionVersionLifecycle(t *testing.T) {
	table := datamodel.Table{ID: uuid.New(), TenantID: uuid.New(), Name: "records"}
	first, changed, err := PrepareIngestion(table, nil, nil, nil, uuid.NewRandom)
	if err != nil || !changed || first.Version == uuid.Nil {
		t.Fatal(first, changed, err)
	}
	// Model database JSON reserialization, including equivalent Unicode escaping.
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, first.Payload, "", "  "); err != nil {
		t.Fatal(err)
	}
	first.Payload = []byte(strings.ReplaceAll(formatted.String(), "records", `\u0072ecords`))
	before := bytes.Clone(first.Payload)
	table.Alias = "Display alias"
	table.Description = "Presentation only"
	unchanged, changed, err := PrepareIngestion(table, nil, nil, &first, func() (uuid.UUID, error) { t.Fatal("unchanged contract allocated token"); return uuid.Nil, nil })
	if err != nil || changed || unchanged.Version != first.Version {
		t.Fatal(unchanged, changed, err)
	}
	unchanged.Payload[0] = ' '
	if !bytes.Equal(first.Payload, before) {
		t.Fatal("stored payload aliased or mutated")
	}
	table.Archived = true
	archived, changed, err := PrepareIngestion(table, nil, nil, &first, uuid.NewRandom)
	if err != nil || !changed || archived.Version == first.Version {
		t.Fatal(archived, changed, err)
	}
	table.Archived = false
	reverted, changed, err := PrepareIngestion(table, nil, nil, &archived, uuid.NewRandom)
	if err != nil || !changed || reverted.Version == first.Version || reverted.Version == archived.Version {
		t.Fatal("reverted payload reused version", err)
	}
}

func TestPrepareIngestionRejectsInvalidPriorAndGenerator(t *testing.T) {
	table := datamodel.Table{ID: uuid.New(), TenantID: uuid.New(), Name: "records"}
	previous, _, err := PrepareIngestion(table, nil, nil, nil, uuid.NewRandom)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"tenant", "table", "version", "json", "utf8", "trailing"} {
		t.Run(kind, func(t *testing.T) {
			p := previous
			switch kind {
			case "tenant":
				p.TenantID = uuid.New()
			case "table":
				p.TableID = uuid.New()
			case "version":
				p.Version = uuid.Nil
			case "json":
				p.Payload = []byte("{")
			case "utf8":
				p.Payload = []byte{'"', 0xff, '"'}
			case "trailing":
				p.Payload = append(bytes.Clone(p.Payload), []byte(" {}")...)
			}
			result, changed, err := PrepareIngestion(table, nil, nil, &p, func() (uuid.UUID, error) { t.Fatal("invalid prior allocated token"); return uuid.Nil, nil })
			if err == nil || changed || result.Payload != nil {
				t.Fatal("invalid prior accepted")
			}
		})
	}
	table.Archived = true
	failure := errors.New("entropy unavailable")
	for _, generate := range []func() (uuid.UUID, error){nil, func() (uuid.UUID, error) { return uuid.Nil, nil }, func() (uuid.UUID, error) { return previous.Version, nil }, func() (uuid.UUID, error) { return uuid.Nil, failure }} {
		result, changed, err := PrepareIngestion(table, nil, nil, &previous, generate)
		if err == nil || changed || result.Payload != nil {
			t.Fatal("invalid generator accepted")
		}
	}
	_, _, err = PrepareIngestion(table, nil, nil, &previous, func() (uuid.UUID, error) { return uuid.Nil, failure })
	if !errors.Is(err, failure) {
		t.Fatal("generator error lost", err)
	}
}

func TestIngestionPayloadSemanticComparison(t *testing.T) {
	for _, test := range []struct {
		name, stored, desired string
		equal                 bool
	}{
		{"object_order", `{"name":"a","archived":false}`, `{"archived":false,"name":"a"}`, true},
		{"escape", `{"name":"\u0061"}`, `{"name":"a"}`, true},
		{"array_order", `{"enum_values":["a","b"]}`, `{"enum_values":["b","a"]}`, false},
		{"null_array", `{"enum_values":null}`, `{"enum_values":[]}`, false},
		{"null_object", `{"fields":null}`, `{"fields":{}}`, false},
		{"missing", `{}`, `{"archived":false}`, false},
		{"extra", `{"archived":false,"extra":true}`, `{"archived":false}`, false},
		{"numeric_string", `{"value":1}`, `{"value":"1"}`, false},
		{"large_number", `{"value":9007199254740993}`, `{"value":"9007199254740993"}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			equal, err := equalIngestionPayload([]byte(test.stored), []byte(test.desired))
			if err != nil || equal != test.equal {
				t.Fatal(equal, err)
			}
		})
	}
}
