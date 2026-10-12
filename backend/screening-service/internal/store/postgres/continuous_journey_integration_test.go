package postgres_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	caseclient "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/riverjobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/postgres"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// This opt-in test starts the actual case-manager server, not a callback fixture.
// Build its cmd/server and set CASE_MANAGER_INTEGRATION_BINARY to the executable.
func TestContinuousProducerToCaseHTTPJourney(t *testing.T) {
	binary := os.Getenv("CASE_MANAGER_INTEGRATION_BINARY")
	if binary == "" {
		t.Skip("set CASE_MANAGER_INTEGRATION_BINARY to a freshly built case-manager server")
	}
	db := deliveryDatabase(t)
	ctx := context.Background()
	tenant, inbox := uuid.NewString(), uuid.NewString()
	// Same logical DB, independently owned schemas and ordered migrations.
	for _, owner := range []string{"data-model-service", "ingestion-service", "decision-engine-service", "case-manager-service"} {
		paths, err := filepath.Glob(filepath.Join("..", "..", "..", "..", owner, "internal", "migrations", "metadata", "*.up.sql"))
		if err != nil || len(paths) == 0 {
			t.Fatal("missing owner migrations", owner, err)
		}
		for _, p := range paths {
			sql, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(ctx, string(sql)); err != nil {
				t.Fatal(p, err)
			}
		}
	}
	if _, err := db.Exec(ctx, `INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'Intake test','intake_test','active',now(),now())`, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO case_manager.inboxes(id,tenant_id,name,created_at,updated_at) VALUES($1,$2,'Review',now(),now())`, inbox, tenant); err != nil {
		t.Fatal(err)
	}
	endpoint, token := startCaseServer(t, db, binary, tenant)
	driver := riverpgxv5.New(db)
	migrator, err := rivermigrate.New(driver, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		t.Fatal(err)
	}
	workers := river.NewWorkers()
	screeningWorker := riverjobs.NewScreeningWorker(dispatch(db, resultProvider(false, 2)))
	river.AddWorker(workers, &screeningWorker)
	client, err := river.NewClient(driver, &river.Config{Workers: workers, Queues: map[string]river.QueueConfig{riverjobs.ScreeningQueueName: {MaxWorkers: 2}, riverjobs.MonitoredObjectQueueName: {MaxWorkers: 2}}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	producer := intakeService(db, riverjobs.NewRiverScreeningEnqueuer(client, riverjobs.IntakeMaxAttempts, riverjobs.ScreeningQueueName))
	continuous := service.NewContinuousWorkerService(store.NewTransactionManager(db), deliveryTestClock{}, store.NewContinuousConfigRepository(db), store.NewMonitoredObjectRepository(db), intakeReader{}, producer)
	monitoredWorker := riverjobs.NewMonitoredObjectWorker(continuous)
	river.AddWorker(workers, &monitoredWorker)
	monitoredEnqueuer := riverjobs.NewRiverMonitoredObjectEnqueuer(client, riverjobs.IntakeMaxAttempts, riverjobs.MonitoredObjectQueueName)
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stop, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := client.Stop(stop); err != nil {
			t.Error(err)
		}
	})
	_, monitored := seedMonitored(t, db, tenant, inbox)
	if err := monitoredEnqueuer.Enqueue(ctx, tenant, monitored, nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		var n int
		return db.QueryRow(ctx, `SELECT count(*) FROM screening.continuous_runs WHERE monitored_object_id=$1`, monitored).Scan(&n) == nil && n == 1
	})
	var screeningID string
	if err := db.QueryRow(ctx, `SELECT screening_id::text FROM screening.continuous_runs WHERE monitored_object_id=$1`, monitored).Scan(&screeningID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		var n int
		return db.QueryRow(ctx, `SELECT count(*) FROM screening.case_event_outbox WHERE kind='requires-review'`).Scan(&n) == nil && n == 1
	})
	var queuedAttempts int
	if err := db.QueryRow(ctx, `SELECT max_attempts FROM river_job WHERE kind=$1`, riverjobs.ScreeningArgs{}.Kind()).Scan(&queuedAttempts); err != nil || queuedAttempts != riverjobs.IntakeMaxAttempts {
		t.Fatal("job retry configuration", queuedAttempts, err)
	}
	repo := store.NewCaseEventRepository(db)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// Actual HTTP rejection persists retry state; no case is created on failed auth.
	wrong := service.NewCaseDeliveryService(repo, caseclient.NewHTTPClient(endpoint, "incorrect-token", time.Second), logger)
	if err := wrong.RunBatch(ctx, 1); err != nil {
		t.Fatal(err)
	}
	var cases int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM case_manager.cases`).Scan(&cases); err != nil || cases != 0 {
		t.Fatal("unauthorized arrival created case", cases, err)
	}
	if _, err := db.Exec(ctx, `UPDATE screening.case_event_outbox SET available_at=now()`); err != nil {
		t.Fatal(err)
	}
	delivery := service.NewCaseDeliveryService(repo, caseclient.NewHTTPClient(endpoint, token, 5*time.Second), logger)
	if err := delivery.RunBatch(ctx, 10); err != nil {
		t.Fatal(err)
	}
	var caseID, status, outcome string
	var assignee *string
	var links int
	if err := db.QueryRow(ctx, `SELECT c.id::text,c.status,c.outcome,c.assigned_to,(SELECT count(*) FROM case_manager.case_screenings WHERE case_id=c.id) FROM case_manager.cases c`).Scan(&caseID, &status, &outcome, &assignee, &links); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || outcome != "unset" || assignee != nil || links != 2 {
		t.Fatal("incorrect pre-review case", status, outcome, assignee, links)
	}
	var sourceStatus string
	if err := db.QueryRow(ctx, `SELECT status FROM screening.case_event_outbox WHERE kind='requires-review'`).Scan(&sourceStatus); err != nil || sourceStatus != "delivered" {
		t.Fatal("arrival not acknowledged", sourceStatus, err)
	}
	// Simulate lost acknowledgement after the receiver committed.
	if _, err := db.Exec(ctx, `UPDATE screening.case_event_outbox SET status='pending',available_at=now() WHERE kind='requires-review'`); err != nil {
		t.Fatal(err)
	}
	if err := delivery.RunBatch(ctx, 10); err != nil {
		t.Fatal(err)
	}
	var match string
	if err := db.QueryRow(ctx, `SELECT id FROM screening.screening_matches WHERE screening_id=$1 LIMIT 1`, screeningID).Scan(&match); err != nil {
		t.Fatal(err)
	}
	if _, err := producer.ReviewMatch(ctx, tenant, match, "confirmed_hit", "Source review", "investigator", false); err != nil {
		t.Fatal(err)
	}
	if err := delivery.RunBatch(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM case_manager.cases`).Scan(&cases); err != nil || cases != 1 {
		t.Fatal("duplicate after retry/review", cases, err)
	}
	if err := db.QueryRow(ctx, `SELECT status FROM case_manager.case_screenings WHERE match_id=$1`, match).Scan(&status); err != nil || status != "confirmed_hit" {
		t.Fatal("review not synchronized", status, err)
	}
	// Completed River jobs are still retained. A later dataset change must not
	// be swallowed by uniqueness on the same monitored-object arguments.
	waitFor(t, func() bool {
		var n int
		return db.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE kind=$1 AND state='completed'`, riverjobs.MonitoredObjectArgs{}.Kind()).Scan(&n) == nil && n == 1
	})
	if _, err := db.Exec(ctx, `UPDATE screening.monitored_objects SET status='pending',updated_at=now() WHERE id=$1`, monitored); err != nil {
		t.Fatal(err)
	}
	if err := monitoredEnqueuer.Enqueue(ctx, tenant, monitored, nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		var n int
		return db.QueryRow(ctx, `SELECT count(*) FROM screening.case_event_outbox WHERE kind='requires-review'`).Scan(&n) == nil && n == 2
	})
	if err := delivery.RunBatch(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM case_manager.cases`).Scan(&cases); err != nil || cases != 2 {
		t.Fatal("later run was dropped or merged", cases, err)
	}
	// Decision-linked results use the same real River producer and HTTP consumer.
	scenario, iteration, decision, config := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO core.scenarios(id,tenant_id,name,trigger_object_type,created_at,updated_at) VALUES($1,$2,'Decision intake','customer',now(),now())`, []any{scenario, tenant}},
		{`INSERT INTO core.scenario_iterations(id,scenario_id,tenant_id,version,status,created_at) VALUES($1,$2,$3,1,'live',now())`, []any{iteration, scenario, tenant}},
		{`INSERT INTO core.decisions(id,tenant_id,scenario_id,scenario_iteration_id,object_id,object_type,outcome,created_at) VALUES($1,$2,$3,$4,'customer-1','customer','review',now())`, []any{decision, tenant, scenario, iteration}},
		{`INSERT INTO core.screening_configs(id,tenant_id,scenario_id,name,allowed_outcomes,provider,config_json,active,created_at,updated_at) VALUES($1,$2,$3,'Review',ARRAY['review'],'test','{}',true,now(),now())`, []any{config, tenant, scenario}},
	} {
		if _, err := db.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	decisionRun, err := producer.CreateScreening(ctx, tenant, screening.SearchRequest{DecisionID: decision, ScenarioID: scenario, ScreeningConfigID: config, Provider: "test", ObjectType: "customer", ObjectID: "customer-1", Queries: []screening.SearchQuery{{Name: "Customer"}}})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		var n int
		return db.QueryRow(ctx, `SELECT count(*) FROM screening.case_event_outbox WHERE kind='requires-review' AND payload->>'screening_id'=$1`, decisionRun.ID).Scan(&n) == nil && n == 1
	})
	if err := delivery.RunBatch(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM case_manager.case_decisions WHERE decision_id=$1`, decision).Scan(&links); err != nil || links != 0 {
		t.Fatal("missing route accepted", links, err)
	}
	if _, err := db.Exec(ctx, `UPDATE core.screening_configs SET review_inbox_id=$1 WHERE id=$2`, inbox, config); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE screening.case_event_outbox SET available_at=now() WHERE payload->>'screening_id'=$1`, decisionRun.ID); err != nil {
		t.Fatal(err)
	}
	if err := delivery.RunBatch(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT c.status,c.type,(SELECT count(*) FROM case_manager.case_screenings WHERE case_id=c.id) FROM case_manager.cases c JOIN case_manager.case_decisions d ON d.case_id=c.id AND d.tenant_id=c.tenant_id WHERE d.decision_id=$1`, decision).Scan(&status, &outcome, &links); err != nil || status != "pending" || outcome != "decision" || links != 2 {
		t.Fatal("decision pre-review intake", status, outcome, links, err)
	}
	if err := db.QueryRow(ctx, `SELECT id FROM screening.screening_matches WHERE screening_id=$1 LIMIT 1`, decisionRun.ID).Scan(&match); err != nil {
		t.Fatal(err)
	}
	if _, err := producer.ReviewMatch(ctx, tenant, match, "no_hit", "Reviewed after case arrival", "investigator", false); err != nil {
		t.Fatal(err)
	}
	if err := delivery.RunBatch(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT status FROM case_manager.case_screenings WHERE match_id=$1`, match).Scan(&status); err != nil || status != "no_hit" {
		t.Fatal("decision review sync", status, err)
	}
	// Cross-tenant calls use the real integration middleware.
	badTenant := uuid.NewString()
	badID := uuid.NewString()
	bad := ports.ScreeningRequiresReviewCommand{Version: 1, TenantID: badTenant, ScreeningID: badID, EventID: uuid.NewSHA1(uuid.NameSpaceURL, []byte("screening.requires-review/v1/"+badTenant+"/"+badID)).String()}
	if err := caseclient.NewHTTPClient(endpoint, token, time.Second).PublishScreeningRequiresReview(ctx, bad); err == nil {
		t.Fatal("cross-tenant callback accepted")
	}
}

func waitFor(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timed out waiting for asynchronous intake")
}
func startCaseServer(t *testing.T, db *pgxpool.Pool, binary, tenant string) (string, string) {
	endpoint, token, _ := startInvestigatorServer(t, db, binary, tenant)
	return endpoint, token
}
func startInvestigatorServer(t *testing.T, db *pgxpool.Pool, binary, tenant string) (string, string, func(string, string, string) string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	dir := t.TempDir()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := json.Marshal(map[string]string{"test": string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))})
	if err != nil {
		t.Fatal(err)
	}
	keysPath := filepath.Join(dir, "keys.json")
	if err := os.WriteFile(keysPath, keys, 0600); err != nil {
		t.Fatal(err)
	}
	const token = "continuous-intake-integration-test-token-32-bytes"
	cmd := exec.Command(binary)
	cmd.Dir = dir
	// Config.ConnString retains the original admin DSN after Database is changed.
	// Construct this DSN explicitly so the child uses only this disposable database.
	dbConfig := db.Config().ConnConfig
	dbURL := url.URL{Scheme: "postgres", Host: net.JoinHostPort(dbConfig.Host, strconv.Itoa(int(dbConfig.Port))), Path: "/" + dbConfig.Database, User: url.UserPassword(dbConfig.User, dbConfig.Password)}
	query := url.Values{}
	if dbConfig.TLSConfig == nil {
		query.Set("sslmode", "disable")
	}
	dbURL.RawQuery = query.Encode()
	cmd.Env = append(os.Environ(), "PORT="+strconv.Itoa(port), "DATABASE_URL="+dbURL.String(), "SERVICE_AUTH_MODE=token", "SERVICE_AUTH_TOKEN="+token, "SERVICE_AUTH_TENANT_IDS="+tenant, "USER_JWT_KEYS_FILE="+keysPath, "USER_JWT_ISSUER=intake-test", "USER_JWT_AUDIENCE=intake-test", "GIN_MODE=release")
	cmd.Env = append(cmd.Env, "DATA_MODEL_SERVICE_URL=http://127.0.0.1:1", "INGESTION_SERVICE_URL=http://127.0.0.1:1")
	log, err := os.Create(filepath.Join(dir, "server.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		log.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = log.Close()
		if t.Failed() {
			data, _ := os.ReadFile(filepath.Join(dir, "server.log"))
			t.Log(string(data))
		}
	})
	endpoint := "http://127.0.0.1:" + strconv.Itoa(port)
	client := http.Client{Timeout: time.Second}
	waitFor(t, func() bool {
		resp, err := client.Get(endpoint + "/readyz")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == 200
	})
	return endpoint, token, func(subject, tenant, role string) string {
		claims := jwt.MapClaims{"sub": subject, "tenant_id": tenant, "roles": []string{role}, "iss": "intake-test", "aud": "intake-test", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix()}
		signed := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		signed.Header["kid"] = "test"
		value, err := signed.SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
}
