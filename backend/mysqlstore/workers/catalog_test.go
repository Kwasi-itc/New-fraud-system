package workers

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestLegacyCatalogAndIdentity(t *testing.T) {
	catalog := LegacyCatalog()
	if len(catalog) != 15 {
		t.Fatalf("job inventory changed: %d", len(catalog))
	}
	seen := map[string]bool{}
	for _, c := range catalog {
		if seen[c.Kind] || c.Owner == "" || c.Queue == "" || c.Timeout <= 0 || c.Unique == "" {
			t.Fatal("incomplete or duplicate contract")
		}
		seen[c.Kind] = true
		fields := map[string]string{}
		if !c.Global {
			fields[c.ResourceField] = "11111111-1111-4111-8111-111111111111"
		}
		if c.TenantInPayload {
			fields["tenant_id"] = "22222222-2222-4222-8222-222222222222"
		}
		payload, _ := json.Marshal(fields)
		got, err := DecodeLegacyIdentity(c.Kind, 0, payload)
		if err != nil || got.Owner != c.Owner || got.Kind != c.Kind || got.Global != c.Global || got.TenantID != fields["tenant_id"] || got.ResourceID != fields[c.ResourceField] {
			t.Fatalf("identity: %+v %v", got, err)
		}
		if _, err := DecodeLegacyIdentity(c.Kind, 1, payload); err == nil {
			t.Fatal("invented payload version accepted")
		}
		if _, err := DecodeLegacyIdentity(c.Kind, 0, []byte(`{"unrecognized":"field"}`)); err == nil {
			t.Fatal("unknown payload accepted")
		}
	}
	for _, payload := range []string{`null`, `[]`, `{}`, `{"index_job_id":null}`, `{"index_job_id":"bad"}`, `{"index_job_id":"00000000-0000-0000-0000-000000000000"}`, `{"index_job_id":"11111111-1111-4111-8111-111111111111","index_job_id":"11111111-1111-4111-8111-111111111111"}`, `{"index_job_id":"11111111-1111-4111-8111-111111111111"} {}`, strings.Repeat(" ", 4097)} {
		if _, err := DecodeLegacyIdentity("index_job_execute", 0, []byte(payload)); err == nil {
			t.Fatalf("invalid payload accepted: %s", payload)
		}
	}
	if _, err := DecodeLegacyIdentity("unregistered", 0, []byte(`{}`)); err == nil {
		t.Fatal("unknown kind accepted")
	}
	catalog[0].Kind = "changed"
	if LegacyCatalog()[0].Kind == "changed" {
		t.Fatal("caller mutated shared catalog")
	}
}

// The inventory cannot silently drift away from owner argument structs. This
// test reads source contracts without importing River into the shared executor.
func TestLegacyCatalogMatchesOwnerPayloads(t *testing.T) {
	owners := map[string]string{"data_model": "data-model-service", "ingestion": "ingestion-service", "decision_engine": "decision-engine-service", "screening": "screening-service", "case_manager": "case-manager-service"}
	foundKinds := map[string]bool{}
	expectedKinds := map[string]bool{}
	for _, c := range LegacyCatalog() {
		expectedKinds[c.Kind] = true
		dir := filepath.Join("..", "..", owners[c.Owner], "internal", "riverjobs")
		if c.Global {
			dir = filepath.Join("..", "..", owners[c.Owner], "internal", "worker")
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		structs := map[string][]string{}
		kindType := ""
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, entry.Name()), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(file, func(node ast.Node) bool {
				if spec, ok := node.(*ast.TypeSpec); ok {
					if st, ok := spec.Type.(*ast.StructType); ok {
						fields := []string{}
						for _, field := range st.Fields.List {
							if field.Tag != nil {
								tag, _ := strconv.Unquote(field.Tag.Value)
								if strings.HasPrefix(tag, "json:") {
									name := strings.TrimPrefix(tag, `json:"`)
									name = strings.SplitN(name, `"`, 2)[0]
									name = strings.SplitN(name, ",", 2)[0]
									fields = append(fields, name)
								}
							}
						}
						structs[spec.Name.Name] = fields
					}
				}
				if fn, ok := node.(*ast.FuncDecl); ok && fn.Name.Name == "Kind" && fn.Recv != nil && fn.Body != nil && len(fn.Body.List) == 1 {
					if ret, ok := fn.Body.List[0].(*ast.ReturnStmt); ok && len(ret.Results) == 1 {
						if literal, ok := ret.Results[0].(*ast.BasicLit); ok {
							kind, _ := strconv.Unquote(literal.Value)
							foundKinds[kind] = true
							if kind == c.Kind {
								if receiver, ok := fn.Recv.List[0].Type.(*ast.Ident); ok {
									kindType = receiver.Name
								}
							}
						}
					}
				}
				return true
			})
		}
		fields, exists := structs[kindType]
		want := 0
		if !c.Global {
			want++
		}
		if c.TenantInPayload {
			want++
		}
		if supportsRetryIdentity(c.Kind) {
			want += 2
		}
		if kindType == "" || !exists || len(fields) != want {
			t.Fatalf("%s: owner payload shape changed: %v", c.Kind, fields)
		}
		for _, field := range fields {
			if supportsRetryIdentity(c.Kind) && (field == "payload_version" || field == "retry_attempt") {
				continue
			}
			if field != c.ResourceField && !(c.TenantInPayload && field == "tenant_id") {
				t.Fatalf("%s: unexpected field %s", c.Kind, field)
			}
		}
	}
	for kind := range foundKinds {
		if !expectedKinds[kind] {
			t.Errorf("owner added an uninventoried work kind: %s", kind)
		}
	}
}

func TestVersionedRetryIdentity(t *testing.T) {
	for _, kind := range []string{"upload_log_execute", "deferred_ingest_execute"} {
		field := "upload_log_id"
		if kind == "deferred_ingest_execute" {
			field = "deferred_ingest_id"
		}
		payload := `{"` + field + `":"11111111-1111-4111-8111-111111111111","payload_version":1,"retry_attempt":3}`
		got, err := DecodeIdentity(kind, 1, []byte(payload))
		if err != nil || got.RetryAttempt != 3 || got.Version != 1 || got.TenantID != "" {
			t.Fatalf("retry identity: %+v %v", got, err)
		}
		if _, err := DecodeLegacyIdentity(kind, 0, []byte(payload)); err == nil {
			t.Fatal("versioned identity silently treated as legacy")
		}
		for _, invalid := range []string{strings.Replace(payload, `"payload_version":1`, `"payload_version":2`, 1), strings.Replace(payload, `"retry_attempt":3`, `"retry_attempt":-1`, 1), strings.Replace(payload, `"retry_attempt":3`, `"retry_attempt":2147483648`, 1), strings.Replace(payload, `,"payload_version":1`, "", 1)} {
			if _, err := DecodeIdentity(kind, 1, []byte(invalid)); err == nil {
				t.Fatal("invalid versioned identity accepted")
			}
		}
	}
}
