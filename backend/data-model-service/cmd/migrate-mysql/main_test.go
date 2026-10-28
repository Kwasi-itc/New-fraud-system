package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
)

func TestValidatePlanWithoutDatabase(t *testing.T) {
	definition, _ := json.Marshal(map[string]string{"Table": "data_model_example", "Create Table": "reviewed fixture definition"})
	p := migrations.Plan{Database: "shared_app", Steps: []migrations.Step{{ID: "create_example", Kind: "TABLE", Name: "data_model_example", SQL: "CREATE TABLE shared_app.data_model_example(id INT)", After: string(definition)}}}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run(context.Background(), []string{"-action", "validate", "-plan", path}, &out); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out.Bytes(), []byte("no database inspected")) {
		t.Fatal(out.String())
	}
	for _, bad := range [][]byte{append(append([]byte(nil), data...), data...), []byte(`{"database":"shared_app","unknown":true}`), []byte(`{}`)} {
		if _, err := decodePlan(bad); err == nil {
			t.Fatal("invalid plan accepted")
		}
	}
	if err := run(context.Background(), []string{"-action", "down", "-plan", path}, &out); err == nil {
		t.Fatal("destructive down accepted")
	}
	for _, user := range []string{"", "root"} {
		if err := run(context.Background(), []string{"-action", "verify", "-plan", path, "-definer-user", user}, &out); err == nil {
			t.Fatal("verification without dedicated definer accepted")
		}
	}
	for _, action := range []string{"up", "status"} {
		if err := run(context.Background(), []string{"-action", action, "-plan", path, "-account-policies", "missing.json"}, &out); err == nil {
			t.Fatal("account policy silently ignored for action", action)
		}
	}
}
