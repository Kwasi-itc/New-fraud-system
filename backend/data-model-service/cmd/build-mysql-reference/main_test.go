package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestFrozenReferenceArtifact(t *testing.T) {
	directory := filepath.Join("..", "..", "testdata", "mysql-reference-fraud-v001")
	data, err := os.ReadFile(filepath.Join(directory, "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	var plan migrations.Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(directory, "provenance.json"))
	if err != nil {
		t.Fatal(err)
	}
	var provenance struct {
		ServerVersion string               `json:"server_version"`
		Database      string               `json:"database"`
		Definer       installation.Definer `json:"definer"`
		PlanSHA256    string               `json:"plan_sha256"`
	}
	if err := json.Unmarshal(raw, &provenance); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if provenance.ServerVersion != "8.4.8" || provenance.Database != plan.Database || provenance.PlanSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("reference provenance mismatch")
	}
	if _, err := installation.Plan(plan.Database, provenance.Definer, plan.Steps); err != nil {
		t.Fatal("frozen source lineage changed: append/review an explicit artifact revision", err)
	}
	create, err := installation.CreateDefiner(provenance.Definer)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := installation.DefinerGrants(plan.Database, provenance.Definer)
	if err != nil {
		t.Fatal(err)
	}
	for file, want := range map[string]string{"create_definer.sql": create + ";\n", "grant_definer.sql": strings.Join(grants, ";\n") + ";\n"} {
		actual, err := os.ReadFile(filepath.Join(directory, file))
		if err != nil || string(actual) != want {
			t.Fatalf("frozen %s differs from reviewed source: %v", file, err)
		}
	}
}

func TestReferenceCommandBoundary(t *testing.T) {
	t.Setenv("DATA_MODEL_REQUIRE_MYSQL_TEST", "")
	for _, args := range [][]string{
		{},
		{"-database", "mysql", "-output", "new-reference"},
		{"-database", "shared_app", "-output", "new-reference"},
		{"-database", "shared_app", "-output", "new-reference", "-definer-user", "root"},
		{"-database", "shared_app", "-output", t.TempDir()},
	} {
		if err := run(context.Background(), args, io.Discard); err == nil {
			t.Fatal("unsafe reference invocation accepted")
		}
	}
	if err := run(context.Background(), []string{"-help"}, io.Discard); err != nil {
		t.Fatal(err)
	}
}

func TestFrozenDeliveryRetryUpgrade(t *testing.T) {
	read := func(version string) (migrations.Plan, []byte) {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "mysql-reference-fraud-"+version, "plan.json"))
		if err != nil {
			t.Fatal(err)
		}
		var plan migrations.Plan
		if err := json.Unmarshal(data, &plan); err != nil {
			t.Fatal(err)
		}
		return plan, data
	}
	previous, _ := read("v001")
	upgraded, data := read("v002")
	if len(upgraded.Steps) != len(previous.Steps)+1 || !reflect.DeepEqual(upgraded.Steps[:len(previous.Steps)], previous.Steps) {
		t.Fatal("upgrade rewrote frozen v001 lineage")
	}
	bound, err := installation.ExtendDeliveryRetryPlan(previous, upgraded.Steps[len(previous.Steps)])
	if err != nil || !reflect.DeepEqual(bound, upgraded) {
		t.Fatalf("upgrade differs from source: %v", err)
	}
	last := upgraded.Steps[len(previous.Steps)]
	last.SQL += " "
	if _, err := installation.ExtendDeliveryRetryPlan(previous, last); err == nil {
		t.Fatal("altered upgrade SQL accepted")
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "mysql-reference-fraud-v002", "provenance.json"))
	if err != nil {
		t.Fatal(err)
	}
	var provenance struct {
		PlanSHA256   string `json:"plan_sha256"`
		ReviewStatus string `json:"review_status"`
	}
	if err := json.Unmarshal(raw, &provenance); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if provenance.PlanSHA256 != hex.EncodeToString(sum[:]) || provenance.ReviewStatus != "needs_review" {
		t.Fatal("upgrade provenance mismatch")
	}
}

func TestReferenceArtifactRoundTrip(t *testing.T) {
	if os.Getenv("DATA_MODEL_REQUIRE_MYSQL_TEST") != "1" {
		t.Skip("isolated reference fixture not required")
	}
	if os.Getenv("DATA_MODEL_MYSQL_TEST_ADDRESS") != "mysql:3306" || os.Getenv("DATA_MODEL_MYSQL_TEST_ROOT_PASSWORD") != "isolated-test-root-only" {
		t.Fatal("only disposable fixture allowed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	database := "reference_artifact_" + suffix
	definer := installation.Definer{User: "guard_" + suffix[:24], Host: "localhost"}
	path := filepath.Join(t.TempDir(), "artifact")
	args := []string{"-database", database, "-definer-user", definer.User, "-definer-host", definer.Host, "-output", path}
	if err := run(ctx, args, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := run(ctx, args, io.Discard); err == nil {
		t.Fatal("reference artifact overwritten")
	}
	data, err := os.ReadFile(filepath.Join(path, "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	var plan migrations.Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	db, err := mysqlstore.Open(ctx, mysqlstore.Config{Address: "mysql:3306", Database: "data_model_publication_test", User: "root", Password: "isolated-test-root-only", AllowInsecureTransport: true, MaxOpen: 2, MaxIdle: 1, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME=?", database).Scan(&count); err != nil || count != 0 {
		t.Fatalf("reference database not cleaned: %d %v", count, err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM mysql.user WHERE User=? AND Host=?", definer.User, definer.Host).Scan(&count); err != nil || count != 0 {
		t.Fatalf("reference principal not cleaned: %d %v", count, err)
	}
	schema, _ := mysqlstore.Identifier(database)
	account, _ := definer.SQLAccount()
	exec := func(q string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	exec("CREATE DATABASE " + schema + " CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin")
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		for _, q := range []string{"DROP DATABASE " + schema, "DROP USER IF EXISTS " + account} {
			if _, err := db.ExecContext(cleanup, q); err != nil {
				t.Error(err)
			}
		}
	}()
	create, err := os.ReadFile(filepath.Join(path, "create_definer.sql"))
	if err != nil {
		t.Fatal(err)
	}
	exec(string(create))
	runner, err := migrations.New(db, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if err := runner.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	grants, err := os.ReadFile(filepath.Join(path, "grant_definer.sql"))
	if err != nil {
		t.Fatal(err)
	}
	// This file contains only generated single-statement GRANTs, no routines.
	for _, grant := range strings.Split(string(grants), ";\n") {
		if grant != "" {
			exec(grant)
		}
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := installation.VerifyDefiner(ctx, conn, database, definer); err != nil {
		t.Fatal(err)
	}
}
