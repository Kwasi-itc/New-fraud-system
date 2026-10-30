package postgres

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var updateInventory = flag.Bool("update-postgres-inventory", false, "explicitly regenerate the decision source inventory after reviewing source changes")

type inventoryFile struct {
	Path                string   `json:"path"`
	SHA256              string   `json:"sha256"`
	QualifiedReferences []string `json:"qualified_references"`
	Features            []string `json:"features"`
	ScanCalls           int      `json:"scan_calls"`
}

// QualifiedReferences are lexical source references, NOT inferred ownership or
// complete dynamic-query expansion. Source hashes include all production files
// and every up/down migration so additions cannot silently escape review.
func TestDecisionPostgresSourceInventory(t *testing.T) {
	var actual []inventoryFile
	ref := regexp.MustCompile(`\b(?:core|screening|public)\.[a-z_][a-z_0-9]*`)
	for _, root := range []string{".", "../../migrations/metadata", "../../ports", "../../tenantdata"} {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			if !(strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go")) && !strings.HasSuffix(path, ".sql") {
				return nil
			}
			source, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			hash := sha256.Sum256(source)
			text := string(source)
			set := map[string]bool{}
			for _, name := range ref.FindAllString(text, -1) {
				set[name] = true
			}
			names := []string{}
			for n := range set {
				names = append(names, n)
			}
			sort.Strings(names)
			features := []string{}
			lower := strings.ToLower(text)
			for _, feature := range []string{"returning", "on conflict", "jsonb", "text[]", "uuid[]", "::", "ilike", "distinct on", "lateral", "for update", "for key share", "skip locked", "pgx", "fmt.sprintf", "jsonb_agg", "reconcile_screening_source"} {
				if strings.Contains(lower, feature) {
					features = append(features, feature)
				}
			}
			actual = append(actual, inventoryFile{filepath.ToSlash(path), hex.EncodeToString(hash[:]), names, features, strings.Count(text, ".Scan(")})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Slice(actual, func(i, j int) bool { return actual[i].Path < actual[j].Path })
	const path = "testdata/postgres_inventory.json"
	if *updateInventory {
		data, err := json.MarshalIndent(actual, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.MkdirAll("testdata", 0755); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, append(data, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("inventory missing; review source and explicitly regenerate with -update-postgres-inventory")
	}
	var expected []inventoryFile
	if err = json.Unmarshal(data, &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatal("decision source inventory drift: review SQL/ports/migrations and regenerate with -update-postgres-inventory")
	}
}
