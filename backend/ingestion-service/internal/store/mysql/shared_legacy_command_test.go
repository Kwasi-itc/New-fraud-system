package mysql

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

func TestFullSharedLegacyOperatorCommands(t *testing.T) {
	binary := os.Getenv("INGESTION_IMPORT_TEST_BINARY")
	if binary == "" {
		t.Fatal("compiled import command required")
	}
	shared, f := fullRecordFixture(t)
	operator := fullFixturePrincipal(t, shared, "importer")
	var account string
	if e := operator.QueryRow("SELECT SUBSTRING_INDEX(CURRENT_USER(),'@',1)").Scan(&account); e != nil {
		t.Fatal(e)
	}
	input := legacyFixture(t, f, "single")
	wire, e := json.Marshal(input)
	if e != nil {
		t.Fatal(e)
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "export.jsonl")
	checkpoint := filepath.Join(directory, "progress.json")
	if e = os.WriteFile(path, append(wire, '\n'), 0600); e != nil {
		t.Fatal(e)
	}
	run := func(mode, checkpoint string) (map[string]int, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		args := []string{"-mode", mode, "-input", path, "-snapshot", "fixture-source-v1", "-database", shared.Database, "-tenant", f.model.TenantID.String(), "-table", f.model.Tables["records"].ID.String(), "-object-type", "records", "-checkpoint", checkpoint, "-isolated-fixture", "-max-records", "1"}
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Env = append(os.Environ(), "INGESTION_IMPORT_MYSQL_ADDRESS=mysql:3306", "INGESTION_IMPORT_MYSQL_USER="+account, "INGESTION_IMPORT_MYSQL_PASSWORD=isolated-reader-only", "INGESTION_IMPORT_OPERATOR_TOKEN="+strings.Repeat("t", 40))
		output, e := cmd.CombinedOutput()
		values := map[string]int{}
		first := strings.SplitN(string(output), "\n", 2)[0]
		var decoded map[string]any
		if err := json.Unmarshal([]byte(first), &decoded); err != nil && e == nil {
			t.Fatal(string(output), err)
		}
		for key, value := range decoded {
			switch v := value.(type) {
			case float64:
				values[key] = int(v)
			case bool:
				if v {
					values[key] = 1
				}
			}
		}
		if e != nil {
			t.Log("expected command error/report:", string(output))
		}
		return values, e
	}
	for _, query := range []string{"SELECT raw_value FROM data_model_exact_keys", "UPDATE data_model_tenants SET status='pending'", "INSERT INTO ingestion_audit(id) VALUES(UUID_TO_BIN(UUID()))", "DELETE FROM ingestion_request_receipts", "CREATE TABLE forbidden(id INT)"} {
		if _, e := operator.Exec(query); e == nil {
			t.Fatal("excess import grant", query)
		}
	}
	before := fullDurableCounts(t, f)
	importerAccount := account
	preview := fullFixturePrincipal(t, shared, "preview")
	if e := preview.QueryRow("SELECT SUBSTRING_INDEX(CURRENT_USER(),'@',1)").Scan(&account); e != nil {
		t.Fatal(e)
	}
	report, e := run("dry-run", checkpoint)
	if e != nil || report["WouldImport"] != 1 {
		t.Fatal(report, e)
	}
	if got := fullDurableCounts(t, f); got != before {
		t.Fatal("dry-run wrote effects", got)
	}
	account = importerAccount
	// Commit succeeds but the checkpoint directory is missing. Retrying from the
	// beginning must reconcile the retained imported result, not duplicate it.
	report, e = run("import", filepath.Join(directory, "missing", "progress.json"))
	if e == nil {
		t.Fatal("checkpoint failure hidden", report)
	}
	got := fullDurableCounts(t, f)
	if got != (fullOutcomeCounts{PublicReceipts: 1}) {
		t.Fatal(got)
	}
	report, e = run("import", checkpoint)
	if e != nil || report["AlreadyImported"] != 1 || report["Imported"] != 0 || report["LastCommittedLine"] != 1 {
		t.Fatal(report, e)
	}
	report, e = run("import", checkpoint)
	if e != nil || report["Read"] != 0 {
		t.Fatal(report, e)
	}
	// A bounded invocation advertises remaining input and resumes at the last
	// committed line, even when a source export repeats an already retained row.
	doubled := append(append(append([]byte{}, wire...), '\n'), append(append([]byte{}, wire...), '\n')...)
	if e = os.WriteFile(path, doubled, 0600); e != nil {
		t.Fatal(e)
	}
	bounded := filepath.Join(directory, "bounded.json")
	report, e = run("import", bounded)
	if e != nil || report["HasMore"] != 1 || report["LastCommittedLine"] != 1 {
		t.Fatal(report, e)
	}
	report, e = run("import", bounded)
	if e != nil || report["HasMore"] != 0 || report["LastCommittedLine"] != 2 || report["Read"] != 1 {
		t.Fatal(report, e)
	}
	// Scope/evidence conflicts remain visible; a dry run never adopts provenance
	// from a different export merely because request content has the same hash.
	input.Audits[0].ID = "changed-source-audit"
	wire, e = json.Marshal(input)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, append(wire, '\n'), 0600); e != nil {
		t.Fatal(e)
	}
	report, e = run("dry-run", checkpoint)
	if e != nil || report["Conflicts"] != 1 {
		t.Fatal(report, e)
	}
	if _, e = run("import", checkpoint); e == nil {
		t.Fatal("changed export resumed")
	}
	input.RawJSON = nil
	wire, e = json.Marshal(input)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, append(wire, '\n'), 0600); e != nil {
		t.Fatal(e)
	}
	report, e = run("dry-run", checkpoint)
	if e != nil || report["Rejected"] != 1 {
		t.Fatal(report, e)
	}
	if got := fullDurableCounts(t, f); got != (fullOutcomeCounts{PublicReceipts: 1}) {
		t.Fatal(got)
	}
	var provenance int
	if e := f.db.QueryRow("SELECT COUNT(*) FROM ingestion_legacy_imports WHERE tenant_id=?", mysqlstore.UUID(f.model.TenantID)).Scan(&provenance); e != nil || provenance != 1 {
		t.Fatal(provenance, e)
	}
}
