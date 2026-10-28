package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
)

func TestFrozenCreationReceiptUpgrade(t *testing.T) {
	directory := filepath.Join("..", "..", "testdata", "mysql-reference-fraud-v003")
	read := func(path string) (migrations.Plan, []byte) {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var plan migrations.Plan
		if err := json.Unmarshal(raw, &plan); err != nil {
			t.Fatal(err)
		}
		return plan, raw
	}
	previous, _ := read(filepath.Join("..", "..", "testdata", "mysql-reference-fraud-v002", "plan.json"))
	plan, raw := read(filepath.Join(directory, "plan.json"))
	if len(plan.Steps) != 85 || !reflect.DeepEqual(plan.Steps[:81], previous.Steps) {
		t.Fatal("v003 rewrites frozen history")
	}
	var provenance struct {
		Definer         installation.Definer   `json:"definer"`
		ReceiptDefiners []installation.Definer `json:"receipt_definers"`
		PlanSHA256      string                 `json:"plan_sha256"`
		ReviewStatus    string                 `json:"review_status"`
	}
	p, err := os.ReadFile(filepath.Join(directory, "provenance.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(p, &provenance); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if provenance.PlanSHA256 != hex.EncodeToString(sum[:]) || provenance.ReviewStatus != "needs_review" || len(provenance.ReceiptDefiners) != 1 {
		t.Fatal("invalid provenance")
	}
	guard := provenance.ReceiptDefiners[0]
	rebound, err := installation.ExtendCreationReceiptPlan(previous, guard, plan.Steps[81:])
	if err != nil || !reflect.DeepEqual(rebound, plan) {
		t.Fatal("source binding", err)
	}
	if _, err := installation.NewExactKeyReadinessWithCreationReceipts(&sql.DB{}, plan, provenance.Definer, guard); err != nil {
		t.Fatal(err)
	}
	create, err := installation.CreateDefiner(guard)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := installation.GrantStatements(plan.Database, installation.CreationReceiptDefinerPolicy(guard))
	if err != nil {
		t.Fatal(err)
	}
	for file, want := range map[string]string{"create_receipt_definer.sql": create + ";\n", "grant_receipt_definer.sql": strings.Join(grants, ";\n") + ";\n"} {
		got, err := os.ReadFile(filepath.Join(directory, file))
		if err != nil || string(got) != want {
			t.Fatal(file, err)
		}
	}
	for _, change := range []func([]migrations.Step){
		func(s []migrations.Step) { s[0].SQL += " " }, func(s []migrations.Step) { s[1].ID = "changed" }, func(s []migrations.Step) { s[2].Name = "changed" }, func(s []migrations.Step) { s[3].Kind = "TABLE" }, func(s []migrations.Step) { s[0].Before = s[0].After }, func(s []migrations.Step) { s[1], s[2] = s[2], s[1] },
	} {
		steps := append([]migrations.Step(nil), plan.Steps[81:]...)
		change(steps)
		if _, err := installation.ExtendCreationReceiptPlan(previous, guard, steps); err == nil {
			t.Fatal("substituted upgrade accepted")
		}
	}
}
