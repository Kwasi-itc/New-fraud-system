package main

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestSuiteSelection(t *testing.T) {
	for _, tc := range []struct{ suite, included, excluded string }{
		{"", "TestFullSharedInstallation", "TestProcessStartupChecks"},
		{"process", "TestProcessStartupChecks", "TestWorkTelemetryCompletionAndPool"},
		{"queue", "TestWorkTelemetryCompletionAndPool", "TestProcessReadContracts"},
		{"queue", "TestFullSharedRedrive", "TestFullSharedInstallation"},
		{"queue", "TestIngestionWorkRecovery", "TestLegacyReceiptTranslation"},
		{"queue", "TestWorkInspectionBoundsAndAuthorization", "TestWorkCSVBounds"},
	} {
		pattern, err := suitePattern(tc.suite)
		if err != nil {
			t.Fatal(err)
		}
		r := regexp.MustCompile(pattern)
		if !r.MatchString(tc.included) || r.MatchString(tc.excluded) {
			t.Fatal(tc, pattern)
		}
	}
	if _, err := suitePattern(".*"); err == nil {
		t.Fatal("arbitrary suite accepted")
	}
}

func TestUnknownSuiteCreatesNoOutput(t *testing.T) {
	t.Setenv("INGESTION_MYSQL_TEST_ADDRESS", "mysql:3306")
	t.Setenv("INGESTION_REQUIRE_MYSQL_TEST", "1")
	t.Setenv("INGESTION_DATA_MODEL_FIXTURE_EXPORTER", "unused")
	t.Setenv("INGESTION_VERIFY_SUITE", "unknown")
	target := filepath.Join(t.TempDir(), "results")
	if err := run(context.Background(), target); err == nil {
		t.Fatal("unknown suite accepted")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("invalid suite created output", err)
	}
}

func TestFullSchemaRequiresImporterBeforeOutput(t *testing.T) {
	t.Setenv("INGESTION_MYSQL_TEST_ADDRESS", "mysql:3306")
	t.Setenv("INGESTION_REQUIRE_MYSQL_TEST", "1")
	t.Setenv("INGESTION_DATA_MODEL_FIXTURE_EXPORTER", "unused")
	t.Setenv("INGESTION_VERIFY_SUITE", "")
	t.Setenv("INGESTION_IMPORT_TEST_BINARY", "")
	target := filepath.Join(t.TempDir(), "results")
	if err := run(context.Background(), target); err == nil {
		t.Fatal("missing importer accepted")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("missing prerequisite created output", err)
	}
}

func TestRequiredSuiteAccounting(t *testing.T) {
	tests := []string{"TestFullSharedOne"}
	good := []event{{Action: "run", Test: tests[0]}, {Action: "pass", Test: tests[0]}, {Action: "pass"}}
	if s, e := summarize(tests, good); e != nil || s.Passed != 1 || s.Executed != 1 {
		t.Fatal(s, e)
	}
	for _, events := range [][]event{nil, good[:1], append(append([]event{}, good...), event{Action: "skip", Test: tests[0] + "/child"}), append(append([]event{}, good...), event{Action: "fail"}), append(append([]event{}, good...), event{Action: "run", Test: tests[0]}), {{Action: "run", Test: "TestUnexpected"}}} {
		if _, e := summarize(tests, events); e == nil {
			t.Fatal("incomplete suite accepted", events)
		}
	}
	if _, e := summarize(nil, nil); e == nil {
		t.Fatal("empty suite accepted")
	}
}

func TestRequiredFixtureMissing(t *testing.T) {
	t.Setenv("INGESTION_MYSQL_TEST_ADDRESS", "")
	target := filepath.Join(t.TempDir(), "results")
	if err := run(context.Background(), target); err == nil {
		t.Fatal("missing fixture accepted")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("created misleading output before fixture validation", err)
	}
}
