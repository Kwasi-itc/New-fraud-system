package service

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/migrationaudit"
)

var updateDecisionMapping = flag.Bool("update-decision-schema-manifest", false, "explicitly regenerate proposed decision mapping from a generated disposable PG database, never a deployed database")

func TestIntegrationDecisionSchemaManifest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	dsn := executionIntegrationDatabaseURL(t)
	runExecutionMetadataMigrations(t, dsn)
	db := executionIntegrationPool(t, ctx, dsn)
	defer db.Close()
	var data []byte
	if err := db.QueryRow(ctx, migrationaudit.CatalogSQL).Scan(&data); err != nil {
		t.Fatal(err)
	}
	var catalog migrationaudit.Catalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve fixture path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	files, queries, err := migrationaudit.Sources(root)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := migrationaudit.Build(catalog, files, queries)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "internal/migrationaudit/testdata/decision-schema-mapping.json")
	if *updateDecisionMapping {
		data, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
	}
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("mapping missing; explicit disposable fixture generation required", err)
	}
	var reviewed migrationaudit.Manifest
	if err := json.Unmarshal(expected, &reviewed); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(manifest, reviewed) {
		t.Fatal("fresh decision catalog/source/proposed mapping differs from review artifact")
	}
	columns, constraints, indexes := 0, 0, 0
	for _, table := range catalog.Tables {
		columns += len(table.Columns)
		constraints += len(table.Constraints)
		indexes += len(table.Indexes)
	}
	t.Logf("decision mapping: tables=%d columns=%d constraints=%d indexes=%d routines=%d triggers=%d query_contracts=%d source_files=%d", len(catalog.Tables), columns, constraints, indexes, len(catalog.Routines), len(catalog.Triggers), len(queries), len(files))
}
