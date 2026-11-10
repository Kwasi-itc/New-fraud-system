package integrationtest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/httpapi"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	screening "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/mysqlscreening"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func upgradeScreeningFixture(t *testing.T, f *caseFixture) {
	t.Helper()
	steps, e := screening.Statements(f.database, "cm_test_screening_guard")
	must(t, e)
	for i, old := range f.screeningPlan.Steps {
		if steps[i].ID != old.ID || steps[i].SQL != old.SQL {
			t.Fatal("screening prefix changed")
		}
		steps[i] = old
	}
	conn, e := f.reference.Conn(f.ctx)
	must(t, e)
	for i := len(f.screeningPlan.Steps); i < len(steps); i++ {
		s := &steps[i]
		if strings.HasPrefix(s.SQL, "ALTER ") {
			s.Before, e = migrations.Definition(f.ctx, conn, f.database, s.Kind, s.Name)
			must(t, e)
		}
		_, e = conn.ExecContext(f.ctx, s.SQL)
		must(t, e)
		s.After, e = migrations.Definition(f.ctx, conn, f.database, s.Kind, s.Name)
		must(t, e)
	}
	conn.Close()
	plan := migrations.Plan{Database: f.database, Owner: "screening", Steps: steps}
	runner, e := migrations.NewForOwner(f.db, "screening", plan)
	must(t, e)
	must(t, runner.Apply(f.ctx))
	must(t, runner.Apply(f.ctx))
	grants, e := screening.DefinerGrants(f.database, "cm_test_screening_guard")
	must(t, e)
	for _, db := range []*sql.DB{f.db, f.reference} {
		for _, q := range grants {
			_, e = db.ExecContext(f.ctx, q)
			must(t, e)
		}
	}
}
func seedPublishedScreening(t *testing.T, f *caseFixture, manual bool) (uuid.UUID, uuid.UUID) {
	t.Helper()
	source, match := uuid.New(), uuid.New()
	_, e := f.db.ExecContext(f.ctx, "INSERT INTO `"+f.database+"`.screening_screenings(id,tenant_id,status,provider,object_type,object_id,request_json,response_json,decision_projection,provider_reference,last_error,is_manual,creator_id,created_at,updated_at) VALUES(?,?,'awaiting_review','opensanctions','Person','synthetic-person',JSON_OBJECT(),JSON_OBJECT(),JSON_OBJECT(),'local-development/catalog-v1','',?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))", id(source), id(f.tenant), manual, id(f.user))
	must(t, e)
	_, e = f.db.ExecContext(f.ctx, "INSERT INTO `"+f.database+"`.screening_matches(tenant_id,screening_id,id,entity_id,provider,name,status,score,payload,created_at,updated_at) VALUES(?,?,?,'local-person-001','opensanctions','Alex Example','pending',1,JSON_OBJECT('synthetic',TRUE),UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))", id(f.tenant), id(source), id(match))
	must(t, e)
	return source, match
}
func TestMySQLCaseScreeningContracts(t *testing.T) {
	f := fixture(t)
	upgradeScreeningFixture(t, f)
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	must(t, e)
	pub, e := x509.MarshalPKIXPublicKey(&key.PublicKey)
	must(t, e)
	verifier, e := access.NewVerifier(map[string][]byte{"screening": pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub})}, "case-test", "case-test")
	must(t, e)
	claims := access.Claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: "case-test", Subject: f.user.String(), Audience: jwt.ClaimStrings{"case-test"}, IssuedAt: jwt.NewNumericDate(time.Now()), ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}, TenantID: f.tenant.String(), SessionID: f.sid.String(), AuthzVersion: 1, Roles: []string{"case_manager_admin"}}
	signed := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed.Header["kid"] = "screening"
	token, e := signed.SignedString(key)
	must(t, e)
	router, e := httpapi.NewMySQLRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), f.db, f.database, httpapi.RouterConfig{UserVerifier: verifier, AuthMode: "token", AuthToken: "private-screening-contract-fixture", ServiceTenantIDs: []uuid.UUID{f.tenant}, EnableWorkflowReceipts: true})
	must(t, e)
	call := func(method, path string, body any, status int, service bool) map[string]any {
		t.Helper()
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, strings.NewReader(string(b)))
		req.Header.Set("Content-Type", "application/json")
		credential := token
		if service {
			credential = "private-screening-contract-fixture"
		}
		req.Header.Set("Authorization", "Bearer "+credential)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != status {
			t.Fatalf("%s %s: %d wanted %d: %s", method, path, rec.Code, status, rec.Body.String())
		}
		out := map[string]any{}
		if rec.Body.Len() > 0 {
			must(t, json.Unmarshal(rec.Body.Bytes(), &out))
		}
		return out
	}
	source, match := seedPublishedScreening(t, f, true)
	prefix := "/v1/tenants/" + f.tenant.String()
	manualBody := map[string]any{"screening_id": source, "candidate_id": match, "inbox_id": f.inbox}
	manual := call("POST", prefix+"/manual-screening-investigations", manualBody, 200, false)
	caseID := manual["case"].(map[string]any)["id"].(string)
	again := call("POST", prefix+"/manual-screening-investigations", manualBody, 200, false)
	if again["case"].(map[string]any)["id"] != caseID {
		t.Fatal("manual retry created another case")
	}
	_, e = f.db.ExecContext(f.ctx, "UPDATE `"+f.database+"`.screening_matches SET status='no_hit',updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=?", id(f.tenant), id(match))
	must(t, e)
	event := uuid.New()
	review := map[string]any{"event_id": event, "tenant_id": f.tenant, "screening_id": source, "match_id": match.String(), "status": "no_hit"}
	call("POST", "/v1/screening-events/reviewed", review, 204, true)
	call("POST", "/v1/screening-events/reviewed", review, 204, true)
	file := uuid.New()
	blob := uuid.New()
	_, e = f.db.ExecContext(f.ctx, "INSERT INTO `"+f.database+"`.screening_local_blobs(tenant_id,id,screening_id,file_name,content_type,file_size,upload_digest,upload_until,content,content_sha256,created_at) VALUES(?,?,?,'synthetic-owner-note.txt','text/plain',4,UNHEX(REPEAT('ab',32)),TIMESTAMPADD(HOUR,1,UTC_TIMESTAMP(6)),'note',UNHEX(SHA2('note',256)),UTC_TIMESTAMP(6))", id(f.tenant), id(blob), id(source))
	must(t, e)
	_, e = f.db.ExecContext(f.ctx, "INSERT INTO `"+f.database+"`.screening_files(tenant_id,id,screening_id,file_name,content_type,file_size,storage_key,uploaded_by,created_at) VALUES(?,?,?,'synthetic-owner-note.txt','text/plain',4,?,?,UTC_TIMESTAMP(6))", id(f.tenant), id(file), id(source), "local-screening/"+blob.String(), f.user.String())
	must(t, e)
	callback := map[string]any{"event_id": file, "tenant_id": f.tenant, "screening_id": source, "file_id": file, "uploaded_by": f.user.String()}
	call("POST", "/v1/screening-events/evidence-uploaded", callback, 404, true)
	publication, _ := json.Marshal(callback)
	_, e = f.db.ExecContext(f.ctx, "INSERT INTO `"+f.database+"`.screening_case_event_outbox(tenant_id,id,kind,payload,next_attempt_at,created_at) VALUES(?,?,'evidence-uploaded',?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))", id(f.tenant), id(file), publication)
	must(t, e)
	callback["uploaded_by"] = "forged-uploader"
	call("POST", "/v1/screening-events/evidence-uploaded", callback, 400, true)
	callback["uploaded_by"] = f.user.String()
	_, e = f.db.ExecContext(f.ctx, "CREATE TRIGGER `"+f.database+"`.reject_evidence_audit BEFORE INSERT ON `"+f.database+"`.case_manager_case_events FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected audit failure'")
	must(t, e)
	call("POST", "/v1/screening-events/evidence-uploaded", callback, 500, true)
	var imported int
	must(t, f.db.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM `"+f.database+"`.case_manager_case_files WHERE tenant_id=? AND case_id=?", id(f.tenant), id(uuid.MustParse(caseID))).Scan(&imported))
	if imported != 0 {
		t.Fatal("failed callback committed evidence")
	}
	_, e = f.db.ExecContext(f.ctx, "DROP TRIGGER `"+f.database+"`.reject_evidence_audit")
	must(t, e)
	statuses := make(chan int, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("POST", "/v1/screening-events/evidence-uploaded", strings.NewReader(string(publication)))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer private-screening-contract-fixture")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			statuses <- rec.Code
		}()
	}
	wg.Wait()
	close(statuses)
	for status := range statuses {
		if status != 204 {
			t.Fatalf("concurrent evidence delivery returned %d", status)
		}
	}
	call("POST", "/v1/screening-events/evidence-uploaded", callback, 204, true)
	attached := call("POST", prefix+"/cases/"+caseID+"/files", map[string]any{"source_file_id": file}, 201, false)
	if attached["file"].(map[string]any)["source_file_id"] != file.String() {
		t.Fatal("source file provenance lost")
	}
	fileID := attached["file"].(map[string]any)["id"].(string)
	must(t, f.db.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM `"+f.database+"`.case_manager_case_files WHERE tenant_id=? AND case_id=?", id(f.tenant), id(uuid.MustParse(caseID))).Scan(&imported))
	if imported != 1 {
		t.Fatal("callback or human replay duplicated imported evidence")
	}
	req := httptest.NewRequest("GET", prefix+"/cases/"+caseID+"/files/"+fileID+"/download", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != "note" {
		t.Fatalf("imported evidence download: %d %s", rec.Code, rec.Body.String())
	}
	call("POST", prefix+"/cases/"+caseID+"/files", map[string]any{"source_file_id": uuid.New()}, 404, false)
	continuous, _ := seedPublishedScreening(t, f, false)
	cfg, object := uuid.New(), uuid.New()
	for _, row := range []struct {
		q string
		a []any
	}{
		{"INSERT INTO `" + f.database + "`.screening_continuous_configs(tenant_id,id,name,object_type,provider,field_map_json,review_inbox_id,enabled,created_at,updated_at) VALUES(?,?,'Synthetic ongoing check','Person','opensanctions',JSON_OBJECT(),?,TRUE,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))", []any{id(f.tenant), id(cfg), id(f.inbox)}},
		{"INSERT INTO `" + f.database + "`.screening_monitored_objects(tenant_id,id,config_id,object_type,object_id,status,attributes_json,created_at,updated_at) VALUES(?,?,?,'Person','synthetic-person','active',JSON_OBJECT(),UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))", []any{id(f.tenant), id(object), id(cfg)}},
		{"INSERT INTO `" + f.database + "`.screening_continuous_runs(tenant_id,screening_id,config_id,monitored_object_id,created_at) VALUES(?,?,?,?,UTC_TIMESTAMP(6))", []any{id(f.tenant), id(continuous), id(cfg), id(object)}},
	} {
		_, e = f.db.ExecContext(f.ctx, row.q, row.a...)
		must(t, e)
	}
	arrivalEvent := uuid.NewSHA1(uuid.NameSpaceURL, []byte("screening.requires-review/v1/"+f.tenant.String()+"/"+continuous.String()))
	arrival := map[string]any{"version": 1, "event_id": arrivalEvent, "tenant_id": f.tenant, "screening_id": continuous}
	payload, _ := json.Marshal(arrival)
	_, e = f.db.ExecContext(f.ctx, "INSERT INTO `"+f.database+"`.screening_case_event_outbox(tenant_id,id,kind,payload,next_attempt_at,created_at) VALUES(?,?,'requires-review',?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))", id(f.tenant), id(arrivalEvent), payload)
	must(t, e)
	call("POST", "/v1/screening-events/requires-review", arrival, 204, true)
	call("POST", "/v1/screening-events/requires-review", arrival, 204, true)
	var count int
	must(t, f.db.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM `"+f.database+"`.case_manager_continuous_bindings WHERE tenant_id=? AND screening_id=?", id(f.tenant), id(continuous)).Scan(&count))
	if count != 1 {
		t.Fatal("arrival did not bind exactly one case")
	}
	arrival["event_id"] = uuid.New()
	call("POST", "/v1/screening-events/requires-review", arrival, 400, true)
	if dir := os.Getenv("CASE_TEST_REPORT_DIR"); dir != "" {
		must(t, os.MkdirAll(dir, 0755))
		b, _ := json.MarshalIndent(map[string]any{"passed": true, "verified_at": time.Now().UTC(), "successful_operations": []string{"POST /v1/tenants/:tenantId/manual-screening-investigations", "POST /v1/tenants/:tenantId/cases/:caseId/files", "POST /v1/screening-events/reviewed", "POST /v1/screening-events/requires-review", "POST /v1/screening-events/evidence-uploaded"}}, "", "  ")
		must(t, os.WriteFile(filepath.Join(dir, "mysql-screening-results.json"), append(b, '\n'), 0644))
	}
}
