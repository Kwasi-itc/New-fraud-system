package metadata

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestPlatformColumnManifestCoversCompleteSourceTables(t *testing.T) {
	var manifest struct {
		Format, Status, Scope string
		SourceFile            string `json:"source_file"`
		SourceSHA             string `json:"source_sha256_lf"`
		Tables                []struct {
			Source, Target string
			Columns        []struct{ Name, Source, Target, Rule string }
			SourceIndexes  []string `json:"source_indexes"`
			TargetIndexes  []string `json:"target_indexes"`
			Auxiliary      []string `json:"auxiliary_columns"`
		}
	}
	data, err := os.ReadFile("testdata/platform-column-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Format != "decision-platform-mapping/v1" || manifest.Status != "needs_review" || manifest.Scope == "" || len(manifest.Tables) != 3 {
		t.Fatal("manifest scope/review state")
	}
	if manifest.SourceFile != "internal/migrations/metadata/000001_init.up.sql" {
		t.Fatal("unreviewed source path")
	}
	source, err := os.ReadFile(filepath.Join("../../../..", manifest.SourceFile))
	if err != nil {
		t.Fatal(err)
	}
	normalized := strings.ReplaceAll(string(source), "\r\n", "\n")
	hash := sha256.Sum256([]byte(normalized))
	if hex.EncodeToString(hash[:]) != manifest.SourceSHA {
		t.Fatal("source migration drift: review all platform column/constraint/index mappings")
	}
	steps, err := PlatformStatements("fraud")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for i, table := range manifest.Tables {
		if steps[i].Name != table.Target {
			t.Fatal("target object not in initial lineage")
		}
		match := regexp.MustCompile(`(?s)CREATE TABLE IF NOT EXISTS ` + regexp.QuoteMeta(table.Source) + ` \(\n(.*?)\n\);`).FindStringSubmatch(string(source))
		if len(match) != 2 {
			t.Fatal("source table not found", table.Source)
		}
		lines := strings.Split(match[1], "\n")
		if len(lines) != len(table.Columns) {
			t.Fatal("unmapped source column/constraint", table.Source)
		}
		for n, column := range table.Columns {
			if strings.TrimSuffix(strings.TrimSpace(lines[n]), ",") != column.Name+" "+column.Source || column.Rule == "" {
				t.Fatal("source mapping drift", table.Source, column.Name)
			}
			if !strings.Contains(steps[i].SQL, " "+column.Name+" "+column.Target) {
				t.Fatal("target type mapping drift", column.Name)
			}
			count++
		}
		for _, index := range table.SourceIndexes {
			if !strings.Contains(string(source), "CREATE INDEX IF NOT EXISTS "+index) {
				t.Fatal("source index mapping drift")
			}
		}
		for _, index := range table.TargetIndexes {
			if !strings.Contains(steps[i].SQL, index) {
				t.Fatal("target index mapping drift", index)
			}
		}
		for _, column := range table.Auxiliary {
			if !strings.Contains(steps[i].SQL, column+" BINARY(32) GENERATED ALWAYS AS") {
				t.Fatal("digest must be generated", column)
			}
		}
		if strings.Contains(steps[i].SQL, "UNIQUE") {
			t.Fatal("hashes must not impose secondary uniqueness")
		}
	}
	if count != 17 {
		t.Fatal("expected all seventeen business columns", count)
	}
}
