package postgres_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/internalprovider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/privatelists"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func acceptanceInventory(t *testing.T, db *pgxpool.Pool) map[string]any {
	t.Helper()
	ctx := context.Background()
	rows, e := db.Query(ctx, `SELECT schemaname,tablename FROM pg_tables WHERE schemaname IN ('core','identity','screening','case_manager','public') ORDER BY 1,2`)
	if e != nil {
		t.Fatal(e)
	}
	names := [][2]string{}
	for rows.Next() {
		var s, n string
		if e = rows.Scan(&s, &n); e != nil {
			t.Fatal(e)
		}
		names = append(names, [2]string{s, n})
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		t.Fatal(e)
	}
	result := map[string]any{}
	for _, name := range names {
		var count int64
		var digest string
		e = db.QueryRow(ctx, `SELECT count(*),md5(coalesce(string_agg(row_json,E'\n' ORDER BY row_json),'')) FROM(SELECT to_jsonb(t)::text AS row_json FROM `+pgx.Identifier{name[0], name[1]}.Sanitize()+` t) q`).Scan(&count, &digest)
		if e != nil {
			t.Fatal(e)
		}
		result[name[0]+"."+name[1]] = map[string]any{"count": count, "content_digest": digest}
	}
	rows, e = db.Query(ctx, `SELECT schemaname,sequencename FROM pg_sequences WHERE schemaname IN ('core','identity','screening','case_manager','public') ORDER BY 1,2`)
	if e != nil {
		t.Fatal(e)
	}
	names = nil
	for rows.Next() {
		var s, n string
		if e = rows.Scan(&s, &n); e != nil {
			t.Fatal(e)
		}
		names = append(names, [2]string{s, n})
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range names {
		var value int64
		var called bool
		if e = db.QueryRow(ctx, `SELECT last_value,is_called FROM `+pgx.Identifier{name[0], name[1]}.Sanitize()).Scan(&value, &called); e != nil {
			t.Fatal(e)
		}
		result["sequence:"+name[0]+"."+name[1]] = map[string]any{"last_value": value, "is_called": called}
	}
	return result
}

func acceptancePGTool(t *testing.T, db *pgxpool.Pool, tool string, args ...string) {
	t.Helper()
	cfg := db.Config().ConnConfig
	// This is an explicitly disposable child's credential. Never place a password
	// or DSN in arguments, report or command text. No application env is selected.
	cmd := exec.Command(tool, append([]string{"--host", cfg.Host, "--port", strconv.Itoa(int(cfg.Port)), "--username", cfg.User, "--dbname", cfg.Database, "--no-password"}, args...)...)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+cfg.Password, "PGSSLMODE=disable", "PGTZ=UTC")
	if raw, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("%s failed: %v: %s", tool, e, raw)
	}
}

func TestAcceptanceLogicalRestorePreservesEvidenceAndResumesWorkers(t *testing.T) {
	if os.Getenv("SCREENING_RUN_RESTORE_TEST") != "true" {
		t.Skip("explicit opt-in; requires pg_dump/pg_restore and fresh API/worker binaries")
	}
	for _, tool := range []string{"pg_dump", "pg_restore"} {
		if _, e := exec.LookPath(tool); e != nil {
			t.Fatal("missing acceptance client tool", tool)
		}
	}
	h := newAcceptanceHarness(t, 0)
	db := h.f.db
	ctx := context.Background()
	p := h.f.a.Principal()
	tenant := p.TenantID.String()
	execSQL := func(sql string, args ...any) {
		t.Helper()
		if _, e := db.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	for _, file := range []string{"schema_v2.sql", "schema_v3.sql", "schema_v4.sql"} {
		raw, e := os.ReadFile("../../../../auth-service/internal/platform/" + file)
		if e != nil {
			t.Fatal(e)
		}
		execSQL(string(raw))
	}
	raw, e := os.ReadFile("../../../scripts/source-role-grants.sql")
	if e != nil {
		t.Fatal(e)
	}
	execSQL(string(raw))
	var fixture struct {
		Version int                 `json:"version"`
		Source  string              `json:"source"`
		Records []sourcedata.Record `json:"records"`
	}
	if e = json.Unmarshal(h.f.input, &fixture); e != nil {
		t.Fatal(e)
	}
	name := ""
	for _, r := range fixture.Records {
		if r.Type == "Person" {
			name = r.Name
			break
		}
	}
	endpoint := h.decisionURL + "/v1/tenants/" + tenant + "/scenarios/" + h.scenario + "/evaluate"
	evaluate := func() string {
		t.Helper()
		status, raw, e := h.request("POST", endpoint, h.token, map[string]any{"object_id": uuid.NewString(), "object_type": "transaction", "fields": map[string]any{"beneficiary_name": name, "amount": 25000, "country": "GH"}})
		if e != nil || status != 200 {
			t.Fatalf("restore decision setup %d %v %s", status, e, raw)
		}
		var out struct {
			Result struct{ Decision struct{ ID string } }
		}
		if json.Unmarshal(raw, &out) != nil || out.Result.Decision.ID == "" {
			t.Fatal("decision response")
		}
		return out.Result.Decision.ID
	}
	historical := evaluate()
	input := map[string]any{"operation_id": uuid.NewString(), "provider": sourcedata.Provider, "profile": sourcedata.Profile, "dataset_selection": []sourcedata.DatasetRef{{Scope: "tenant", ID: h.f.private.Dataset}}, "name": name, "entity_type": "Person", "threshold": 100, "limit": 10}
	status, body, e := h.request("POST", h.screeningURL+"/v1/manual-screenings", h.human, input)
	if e != nil || status != 202 {
		t.Fatalf("history setup %d %v %s", status, e, body)
	}
	var origin struct{ ID string }
	if json.Unmarshal(body, &origin) != nil || origin.ID == "" {
		t.Fatal("history ID")
	}
	waitFor(t, func() bool {
		var n int
		return db.QueryRow(ctx, `SELECT count(*) FROM screening.screenings WHERE id=$1 AND completed_at IS NOT NULL`, origin.ID).Scan(&n) == nil && n == 1
	})
	for _, enabled := range []bool{true, false} {
		status, body, e = h.request("POST", h.screeningURL+"/v1/manual-screenings/"+origin.ID+"/monitor", h.human, map[string]bool{"enabled": enabled})
		if e != nil || status != 200 {
			t.Fatalf("monitor audit %d %v %s", status, e, body)
		}
	}
	execSQL(`UPDATE identity.memberships SET roles=ARRAY['tenant_admin','case_investigator'] WHERE user_id=$1 AND tenant_id=$2`, p.Subject, p.TenantID)
	reviewer := identity.Principal{Subject: uuid.NewString(), TenantID: p.TenantID, SessionID: uuid.New(), AuthzVersion: 1}
	execSQL(`INSERT INTO identity.users(id,display_name) VALUES($1,'Restore fixture reviewer')`, reviewer.Subject)
	execSQL(`INSERT INTO identity.memberships(user_id,tenant_id,roles) VALUES($1,$2,ARRAY['tenant_admin'])`, reviewer.Subject, p.TenantID)
	execSQL(`INSERT INTO identity.sessions(id,user_id,tenant_id,digest,expires_at,idle_until) VALUES($1,$2,$3,$4,now()+interval '1 hour',now()+interval '1 hour')`, reviewer.SessionID, reviewer.Subject, p.TenantID, uuid.NewString())
	lists := privatelists.Service{DB: db, Policy: privatelists.TenantAdministratorPolicy()}
	list, e := lists.Create(ctx, p, privatelists.CreateInput{OperationID: uuid.NewString(), Name: "Restore fixture list", Purpose: "Disposable restore verification", RightsReference: "Authored fictional records"})
	if e != nil {
		t.Fatal(e)
	}
	item, e := lists.Submit(ctx, p, list, privatelists.ImportInput{OperationID: uuid.NewString(), CSV: "record_id,name,entity_type,source_reference\nrestore-private,Restore Fixture Company,Organization,own fictional records\n"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = lists.Approve(ctx, p, list, item.ID); e == nil {
		t.Fatal("self approval accepted")
	}
	if _, e = lists.Approve(ctx, reviewer, list, item.ID); e != nil {
		t.Fatal(e)
	}
	fixture.Records = append(fixture.Records, sourcedata.Record{ID: "restore-addition", Name: "Restore Added Fixture", Type: "Person", SourceReference: "Own fixture"})
	changed, e := json.Marshal(fixture)
	if e != nil {
		t.Fatal(e)
	}
	ingest := sourceingest.Store{DB: db}
	latest, e := ingest.Stage(ctx, h.f.private, bytes.NewReader(changed))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ingest.Publish(ctx, h.f.private, latest); e != nil {
		t.Fatal(e)
	}
	waitFor(t, func() bool {
		var n int
		return db.QueryRow(ctx, `SELECT count(*) FROM core.outbox_events WHERE status<>'sent'`).Scan(&n) == nil && n == 0
	})
	for _, stop := range h.stopWorkers {
		stop()
	}
	pendingDecision := evaluate()
	abandoned := uuid.New()
	execSQL(`INSERT INTO screening.source_versions(owner_scope,dataset_id,id,parent_generation,parent_revision,status,actor,normalizer,lease_until) SELECT owner_scope,id,$3,active_generation,publication_revision,'staging','restore-fixture',$4,now()-interval '1 minute' FROM screening.source_datasets WHERE owner_scope=$1 AND id=$2`, h.f.private.Owner, h.f.private.Dataset, abandoned, sourcedata.Normalizer)
	before := acceptanceInventory(t, db)
	backup := filepath.Join(t.TempDir(), "shared-database.dump")
	begin := time.Now()
	acceptancePGTool(t, db, "pg_dump", "--format=custom", "--no-owner", "--file", backup)
	dumpTime := time.Since(begin)
	dump, e := os.ReadFile(backup)
	if e != nil || len(dump) < 5 || string(dump[:5]) != "PGDMP" {
		t.Fatal("invalid logical backup", e)
	}
	sum := sha256.Sum256(dump)
	adminCfg, e := pgxpool.ParseConfig(os.Getenv("SCREENING_TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	admin, e := pgxpool.NewWithConfig(ctx, adminCfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(admin.Close)
	target := "screening_restore_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{target}.Sanitize()); e != nil {
		t.Fatal(e)
	}
	restoreCfg := db.Config()
	restoreCfg.ConnConfig.Database = target
	restored, e := pgxpool.NewWithConfig(ctx, restoreCfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		restored.Close()
		if !strings.HasPrefix(target, "screening_restore_test_") {
			t.Error("unsafe restore cleanup target")
			return
		}
		if _, e := admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{target}.Sanitize()+" WITH(FORCE)"); e != nil {
			t.Error(e)
		}
	})
	var occupied int
	if e = restored.QueryRow(ctx, `SELECT count(*) FROM pg_tables WHERE schemaname NOT IN ('pg_catalog','information_schema')`).Scan(&occupied); e != nil || occupied != 0 {
		t.Fatal("restore target is not empty", occupied, e)
	}
	begin = time.Now()
	acceptancePGTool(t, restored, "pg_restore", "--single-transaction", "--exit-on-error", backup)
	restoreTime := time.Since(begin)
	after := acceptanceInventory(t, restored)
	if !reflect.DeepEqual(before, after) {
		for table, v := range before {
			if !reflect.DeepEqual(v, after[table]) {
				t.Log("restore mismatch", table, v, after[table])
			}
		}
		t.Fatal("restored table/sequence inventory differs")
	}
	if e = privatelists.CheckDatabaseBinding(ctx, db, restored); e == nil {
		t.Fatal("copied database UUID incorrectly authorized a different restore target")
	}
	if e = privatelists.CheckDatabaseBinding(ctx, restored, restored); e != nil {
		t.Fatal("same restored primary rejected", e)
	}
	var publicExecute bool
	if e = restored.QueryRow(ctx, `SELECT coalesce((SELECT bool_or(a.privilege_type='EXECUTE') FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE p.oid='identity.authorize_roles_v1(text,uuid,uuid,bigint,text[],boolean)'::regprocedure AND a.grantee=0),false)`).Scan(&publicExecute); e != nil || publicExecute {
		t.Fatal("restored auth definer ACL", publicExecute, e)
	}
	found, e := (internalprovider.Provider{DB: restored}).Search(ctx, h.f.a, internalRequest("Restore Added Fixture", "Person", sourcedata.DatasetRef{Scope: "tenant", ID: h.f.private.Dataset}))
	if e != nil || len(found.Matches) != 1 {
		t.Fatal("restored current search", e)
	}
	var historicalEvidence []byte
	if e = restored.QueryRow(ctx, `SELECT provider_evidence FROM screening.inline_evidence WHERE decision_id=$1`, historical).Scan(&historicalEvidence); e != nil || !bytes.Contains(historicalEvidence, []byte(h.f.version.String())) {
		t.Fatal("historical generation not retained", e)
	}
	recovered, e := (sourceingest.Store{DB: restored}).RecoverInterrupted(ctx, 50)
	if e != nil || recovered != 1 {
		t.Fatal("restored interrupted staging recovery", recovered, e)
	}
	rollback, e := (sourceingest.Store{DB: restored}).Rollback(ctx, h.f.private, h.f.version)
	if e != nil || rollback.Generation == h.f.version {
		t.Fatal("new-generation rollback after restore", e)
	}
	caseURL, _ := startCaseServer(t, restored, os.Getenv("CASE_MANAGER_INTEGRATION_BINARY"), tenant)
	decisionURL, _, _ := startInvestigatorServer(t, restored, os.Getenv("DECISION_INTEGRATION_BINARY"), tenant, "DATA_MODEL_SERVICE_URL="+h.modelURL, "INTERNAL_SCREENING_INLINE_ENABLED=true", "INTERNAL_SCREENING_FAILURE_POLICY=error")
	sourceURL, _, _ := startInvestigatorServer(t, restored, os.Getenv("SCREENING_INTEGRATION_BINARY"), tenant, "USER_JWT_KEYS_FILE="+h.keysFile, "USER_REQUIRE_SESSION=true", "SCREENING_PROVIDER_URL=")
	startJourneyWorker(t, restored, os.Getenv("SCREENING_WORKER_INTEGRATION_BINARY"), "SCREENING_PROVIDER_URL=", "CASE_SERVICE_URL="+caseURL, "CASE_SERVICE_AUTH_TOKEN="+h.token, "DECISION_ENGINE_URL="+decisionURL)
	startJourneyWorker(t, restored, os.Getenv("DECISION_WORKER_INTEGRATION_BINARY"), "OUTBOX_PUBLISHER_URL="+h.receiverURL+"/events", "SCREENING_SERVICE_URL="+sourceURL)
	begin = time.Now()
	waitFor(t, func() bool {
		var n int
		return restored.QueryRow(ctx, `SELECT(SELECT count(*) FROM core.workflow_executions WHERE decision_id=$1 AND status<>'dispatched')+(SELECT count(*) FROM core.outbox_events WHERE status<>'sent')`, pendingDecision).Scan(&n) == nil && n == 0
	})
	replayTime := time.Since(begin)
	if _, e = restored.Exec(ctx, `UPDATE screening.inline_evidence SET lookup_status='no_candidates' WHERE decision_id=$1`, historical); e == nil {
		t.Fatal("restored immutable trigger missing")
	}
	if _, e = (privatelists.Service{DB: restored, Policy: privatelists.TenantAdministratorPolicy()}).GetImport(ctx, h.f.b.Principal(), list, item.ID); e == nil {
		t.Fatal("restored cross-tenant private access")
	}
	acceptanceReport(t, "logical-restore", map[string]any{"source_scope": "New disposable shared-schema database; synthetic data and generated test identities only", "target_scope": "New empty disposable database; no overwrite/clean flags", "backup_sha256": hex.EncodeToString(sum[:]), "backup_bytes": len(dump), "dump_ms": dumpTime.Milliseconds(), "restore_ms": restoreTime.Milliseconds(), "pending_effect_replay_ms": replayTime.Milliseconds(), "inventories_equal_before_recovery": true, "table_and_sequence_inventories": before, "auth_definer_public_execute": publicExecute, "historical_inline_generation_preserved": true, "current_search_verified": true, "expired_staging_recovered": recovered, "new_generation_rollback_verified": true, "pending_worker_effects_replayed": true, "immutable_trigger_after_restore": true, "cross_tenant_private_read_denied": true, "production_recovery_accepted": false, "limitations": []string{"Logical dump of a quiesced development fixture, not a managed-service snapshot or WAL/PITR test", "No RPO guarantee beyond this consistent backup snapshot; no geographic restore, encrypted storage or credential recovery acceptance", "Production RTO requires deployment data volume, owner-role mapping, network, storage and restart qualification"}})
}
