package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Runs both real owner HTTP servers against one disposable shared database.
func TestInvestigatorOwnerHTTPJourneys(t *testing.T) {
	binaries := map[string]string{"core": os.Getenv("DECISION_INTEGRATION_BINARY"), "screening": os.Getenv("SCREENING_INTEGRATION_BINARY")}
	for _, binary := range binaries {
		if binary == "" {
			t.Skip("set freshly built DECISION_INTEGRATION_BINARY and SCREENING_INTEGRATION_BINARY")
		}
	}
	db := deliveryDatabase(t)
	ctx := context.Background()
	execSQL := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, owner := range []string{"data-model-service", "ingestion-service", "decision-engine-service", "case-manager-service"} {
		paths, err := filepath.Glob(filepath.Join("..", "..", "..", "..", owner, "internal", "migrations", "metadata", "*.up.sql"))
		if err != nil || len(paths) == 0 {
			t.Fatal(owner, err)
		}
		for _, p := range paths {
			raw, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			execSQL(string(raw))
		}
	}
	tenant, inbox, caseID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	execSQL(`INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'Review','review_test','active',now(),now())`, tenant)
	execSQL(`INSERT INTO case_manager.inboxes(id,tenant_id,name,created_at,updated_at) VALUES($1,$2,'Review',now(),now())`, inbox, tenant)
	execSQL(`INSERT INTO case_manager.inbox_users(id,tenant_id,inbox_id,user_id,created_at) VALUES($1,$2,$3,'analyst',now())`, uuid.NewString(), tenant, inbox)
	execSQL(`INSERT INTO case_manager.cases(id,tenant_id,inbox_id,name,status,outcome,type,created_at,updated_at) VALUES($1,$2,$3,'Review','pending','unset','decision',now(),now())`, caseID, tenant, inbox)
	scenario, iteration := uuid.NewString(), uuid.NewString()
	execSQL(`INSERT INTO core.scenarios(id,tenant_id,name,trigger_object_type,created_at,updated_at) VALUES($1,$2,'Review','customer',now(),now())`, scenario, tenant)
	execSQL(`INSERT INTO core.scenario_iterations(id,scenario_id,tenant_id,version,status,created_at) VALUES($1,$2,$3,1,'live',now())`, iteration, scenario, tenant)
	seed := func(owner string) string {
		if owner == "core" {
			id := uuid.NewString()
			execSQL(`INSERT INTO core.decisions(id,tenant_id,scenario_id,scenario_iteration_id,object_id,object_type,outcome,created_at) VALUES($1,$2,$3,$4,'customer-1','customer','review',now())`, id, tenant, scenario, iteration)
			execSQL(`INSERT INTO case_manager.case_decisions(id,tenant_id,case_id,decision_id,created_at) VALUES($1,$2,$3,$4,now())`, uuid.NewString(), tenant, caseID, id)
			return id
		}
		screening, matches := seedReview(t, db, tenant, 1)
		execSQL(`INSERT INTO case_manager.case_screenings(id,tenant_id,case_id,screening_id,match_id,status,created_at) VALUES($1,$2,$3,$4,$5,'pending',now())`, uuid.NewString(), tenant, caseID, screening, matches[0])
		return matches[0]
	}
	client := http.Client{Timeout: 10 * time.Second}
	for _, owner := range []string{"core", "screening"} {
		t.Run(owner, func(t *testing.T) {
			var projectionFailure atomic.Bool
			if owner == "screening" {
				callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if projectionFailure.Load() {
						w.WriteHeader(503)
						return
					}
					w.WriteHeader(200)
				}))
				defer callback.Close()
				t.Setenv("DECISION_ENGINE_URL", callback.URL)
			}
			endpoint, serviceToken, sign := startInvestigatorServer(t, db, binaries[owner], tenant)
			token := sign("analyst", tenant, "case_investigator")
			path := endpoint + "/v1/investigator/tenants/" + tenant + "/cases/" + caseID + "/review"
			request := func(token, resource string, body any) (int, identity.State, string) {
				method, url := http.MethodGet, path+"?resource_id="+resource
				var raw []byte
				if body != nil {
					method = http.MethodPost
					raw, _ = json.Marshal(body)
				}
				req, _ := http.NewRequest(method, url, bytes.NewReader(raw))
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("Content-Type", "application/json")
				resp, err := client.Do(req)
				if err != nil {
					return 0, identity.State{}, err.Error()
				}
				defer resp.Body.Close()
				data, _ := io.ReadAll(resp.Body)
				var state identity.State
				_ = json.Unmarshal(data, &state)
				return resp.StatusCode, state, string(data)
			}
			check := func(want int, token, resource string, body any) identity.State {
				t.Helper()
				code, state, raw := request(token, resource, body)
				if code != want {
					t.Fatalf("want %d got %d: %s", want, code, raw)
				}
				return state
			}
			resource := seed(owner)
			if owner == "screening" {
				execSQL(`UPDATE screening.screenings SET decision_id=$2,screening_config_id=$3 WHERE id=(SELECT screening_id FROM screening.screening_matches WHERE id=$1)`, resource, seed("core"), uuid.NewString())
			}
			state := check(200, token, resource, nil)
			if !state.Eligible || state.Status != "pending" {
				t.Fatal(state)
			}
			check(401, serviceToken, resource, nil)
			check(401, sign("analyst", tenant, "viewer"), resource, nil)
			check(403, sign("stranger", tenant, "case_investigator"), resource, nil)
			check(403, sign("analyst", uuid.NewString(), "case_manager_admin"), resource, nil)
			check(403, token, uuid.NewString(), nil)
			action := "approve"
			if owner == "screening" {
				action = "confirmed_hit"
			}
			cmd := identity.Command{OperationID: uuid.New(), ResourceID: resource, ExpectedVersion: state.Version, Action: action, Findings: "Verified finding"}
			check(400, token, resource, map[string]any{"actor_id": "forged"})
			bad := cmd
			bad.Findings = " "
			check(400, token, resource, bad)
			bad = cmd
			bad.ExpectedVersion = "stale"
			check(409, token, resource, bad)
			bad = cmd
			bad.Action = "whitelist"
			check(400, token, resource, bad)
			// A case-history failure must roll back the owner row, match and comments.
			execSQL(`CREATE FUNCTION case_manager.fail_review_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected failure'; END $$; CREATE TRIGGER fail_review_test BEFORE INSERT ON case_manager.outbox_events FOR EACH ROW EXECUTE FUNCTION case_manager.fail_review_test()`)
			check(500, token, resource, cmd)
			state = check(200, token, resource, nil)
			if state.Status != "pending" {
				t.Fatal("partial owner write", state)
			}
			var n int
			if err := db.QueryRow(ctx, `SELECT count(*) FROM `+owner+`.investigator_reviews WHERE resource_id=$1`, resource).Scan(&n); err != nil || n != 0 {
				t.Fatal(n, err)
			}
			execSQL(`DROP TRIGGER fail_review_test ON case_manager.outbox_events; DROP FUNCTION case_manager.fail_review_test()`)
			if owner == "screening" {
				projectionFailure.Store(true)
				check(200, token, resource, cmd)
				committed := check(200, token, resource, nil)
				if committed.Status != action {
					t.Fatal("unavailable callback blocked durable review", committed)
				}
				projectionFailure.Store(false)
			}
			check(200, token, resource, cmd)
			check(200, token, resource, cmd)
			bad = cmd
			bad.Findings = "Changed"
			check(409, token, resource, bad)
			bad = cmd
			bad.OperationID = uuid.New()
			check(409, token, resource, bad)
			var actor, note, status string
			if err := db.QueryRow(ctx, `SELECT user_id,additional_note,new_value FROM case_manager.case_events WHERE resource_id=$1`, resource).Scan(&actor, &note, &status); err != nil || actor != "analyst" || note != cmd.Findings || status != action {
				t.Fatal(actor, note, status, err)
			}
			if err := db.QueryRow(ctx, `SELECT count(*) FROM case_manager.case_events WHERE resource_id=$1`, resource).Scan(&n); err != nil || n != 1 {
				t.Fatal("duplicate event", n, err)
			}
			if owner == "core" {
				if err := db.QueryRow(ctx, `SELECT outcome FROM core.decisions WHERE id=$1`, resource).Scan(&status); err != nil || status != "review" {
					t.Fatal("machine outcome changed", status, err)
				}
			}
			for _, sql := range []string{`UPDATE ` + owner + `.investigator_reviews SET findings='tampered' WHERE resource_id=$1`, `DELETE FROM ` + owner + `.investigator_reviews WHERE resource_id=$1`} {
				if _, err := db.Exec(ctx, sql, resource); err == nil {
					t.Fatal("mutable review history")
				}
			}
			// Closed case blocks a new command but can acknowledge an existing operation.
			fresh := seed(owner)
			freshState := check(200, token, fresh, nil)
			execSQL(`UPDATE case_manager.cases SET status='closed' WHERE id=$1`, caseID)
			check(200, token, resource, cmd)
			next := identity.Command{OperationID: uuid.New(), ResourceID: fresh, ExpectedVersion: freshState.Version, Action: action, Findings: "New"}
			check(400, token, fresh, next)
			execSQL(`UPDATE case_manager.cases SET status='pending' WHERE id=$1`, caseID)
			execSQL(`DELETE FROM case_manager.inbox_users WHERE inbox_id=$1 AND user_id='analyst'`, inbox)
			check(403, token, resource, cmd)
			execSQL(`INSERT INTO case_manager.inbox_users(id,tenant_id,inbox_id,user_id,created_at) VALUES($1,$2,$3,'analyst',now())`, uuid.NewString(), tenant, inbox)
			// Competing first reviews cannot both commit.
			var wg sync.WaitGroup
			codes := make(chan int, 2)
			for i := 0; i < 2; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					other := next
					other.OperationID = uuid.New()
					code, _, _ := request(token, fresh, other)
					codes <- code
				}()
			}
			wg.Wait()
			close(codes)
			counts := map[int]int{}
			for code := range codes {
				counts[code]++
			}
			if counts[200] != 1 || counts[409] != 1 {
				t.Fatal("competing reviews", counts)
			}
			// Force a command to wait behind a lifecycle or membership mutation.
			// The source must remain pending after the earlier mutation commits.
			for _, mutation := range []string{"close", "revoke", "unlink"} {
				pending := seed(owner)
				version := check(200, token, pending, nil).Version
				tx, err := db.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				lock := `SELECT id FROM case_manager.cases WHERE id=$1 FOR UPDATE`
				lockID := caseID
				if mutation == "revoke" {
					lock = `SELECT id FROM case_manager.inboxes WHERE id=$1 FOR UPDATE`
					lockID = inbox
				}
				if _, err = tx.Exec(ctx, lock, lockID); err != nil {
					tx.Rollback(ctx)
					t.Fatal(err)
				}
				done := make(chan int, 1)
				go func() {
					code, _, _ := request(token, pending, identity.Command{OperationID: uuid.New(), ResourceID: pending, ExpectedVersion: version, Action: action, Findings: "Race"})
					done <- code
				}()
				// Observe a real waiter; do not rely on a scheduling sleep.
				deadline := time.Now().Add(5 * time.Second)
				waiting := false
				for time.Now().Before(deadline) {
					if err = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%case_manager.%')`).Scan(&waiting); err != nil {
						break
					}
					if waiting {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				if !waiting {
					tx.Rollback(ctx)
					t.Fatal("review did not wait behind mutation", err)
				}
				want := 403
				switch mutation {
				case "close":
					_, err = tx.Exec(ctx, `UPDATE case_manager.cases SET status='closed' WHERE id=$1`, caseID)
					want = 400
				case "revoke":
					_, err = tx.Exec(ctx, `DELETE FROM case_manager.inbox_users WHERE inbox_id=$1 AND user_id='analyst'`, inbox)
				case "unlink":
					if owner == "core" {
						_, err = tx.Exec(ctx, `DELETE FROM case_manager.case_decisions WHERE decision_id=$1`, pending)
					} else {
						_, err = tx.Exec(ctx, `DELETE FROM case_manager.case_screenings WHERE match_id=$1`, pending)
					}
				}
				if err != nil {
					tx.Rollback(ctx)
					t.Fatal(err)
				}
				if err = tx.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				if code := <-done; code != want {
					t.Fatalf("%s race: want %d got %d", mutation, want, code)
				}
				if err = db.QueryRow(ctx, `SELECT count(*) FROM `+owner+`.investigator_reviews WHERE resource_id=$1`, pending).Scan(&n); err != nil || n != 0 {
					t.Fatal("unauthorized race committed", n, err)
				}
				if mutation == "close" {
					execSQL(`UPDATE case_manager.cases SET status='pending' WHERE id=$1`, caseID)
				}
				if mutation == "revoke" {
					execSQL(`INSERT INTO case_manager.inbox_users(id,tenant_id,inbox_id,user_id,created_at) VALUES($1,$2,$3,'analyst',now())`, uuid.NewString(), tenant, inbox)
				}
			}
		})
	}
}
