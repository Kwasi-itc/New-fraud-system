package platform

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	authtest "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth/authtest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A real proof exchange returns one token for ingestion and decision APIs.
// Application processes and model/persistence contracts use the disposable DB.
func TestExternalClientProofToIngestionJourney(t *testing.T) {
	binary := os.Getenv("INGESTION_INTEGRATION_BINARY")
	decisionBinary, modelBinary := os.Getenv("DECISION_INTEGRATION_BINARY"), os.Getenv("DATA_MODEL_INTEGRATION_BINARY")
	if binary == "" || decisionBinary == "" || modelBinary == "" {
		t.Skip("fresh ingestion, decision and Data Model API binaries required for multi-API acceptance")
	}
	db := authtest.SharedDatabase(t)
	s := Store{DB: db}
	ctx := context.Background()
	migrator, err := rivermigrate.New(riverpgxv5.New(db), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		t.Fatal(err)
	}
	tenant, table := uuid.New(), uuid.New()
	schema := "t_" + strings.ReplaceAll(tenant.String(), "-", "")
	execSQL := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	execSQL(`INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'Client journey',$2,'active',now(),now())`, tenant, schema)
	execSQL(`INSERT INTO core.model_tables(id,tenant_id,name,created_at,updated_at) VALUES($1,$2,'transactions',now(),now())`, table, tenant)
	for _, field := range []struct{ name, kind string }{{"object_id", "string"}, {"updated_at", "timestamp"}, {"amount", "float"}} {
		execSQL(`INSERT INTO core.model_fields(id,tenant_id,table_id,name,data_type,created_at,updated_at) VALUES($1,$2,$3,$4,$5,now(),now())`, uuid.New(), tenant, table, field.name, field.kind)
	}
	execSQL(fmt.Sprintf(`CREATE SCHEMA %s; CREATE TABLE %s(id uuid PRIMARY KEY,object_id text UNIQUE NOT NULL,updated_at timestamptz NOT NULL,amount double precision NOT NULL)`, pgx.Identifier{schema}.Sanitize(), pgx.Identifier{schema, "transactions"}.Sanitize()))
	_, opaque := provision(t, s, tenant, "client-admin", "tenant_admin")
	session, err := s.Resolve(ctx, opaque)
	if err != nil {
		t.Fatal(err)
	}
	client, err := s.SaveClient(ctx, principal(session), uuid.Nil, ClientChange{Name: "Customer integration", Scopes: []string{"ingest:write", "decisions:evaluate", "decisions:read"}, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	key, pub := clientTestKey(t)
	if err := s.RegisterClientKey(ctx, principal(session), ClientKey{ClientID: client.ID, KeyID: "customer-key", PublicKeyPEM: pub}); err != nil {
		t.Fatal(err)
	}
	platformKey, platformPub := clientTestKey(t)
	owner := Server{Store: s, Key: platformKey, KeyID: "journey-platform", Issuer: auth.PlatformIssuer, Audience: auth.PlatformAudience, Replay: auth.NewReplayCache(100), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	server := httptest.NewUnstartedServer(nil)
	owner.TokenEndpointURL = "http://" + server.Listener.Addr().String() + "/oauth/token"
	server.Config.Handler = owner.Handler()
	server.Start()
	defer server.Close()
	signer := authtest.ServiceSigner(t, "ingestion-service")
	der, err := x509.MarshalPKIXPublicKey(&signer.Key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	platformTrust := auth.TrustedKey{ID: owner.KeyID, PublicKey: platformPub, Issuer: auth.PlatformIssuer, PrincipalTypes: []auth.PrincipalType{auth.PrincipalUser, auth.PrincipalClient}, Audiences: []string{auth.PlatformAudience, auth.ClientAudience}}
	ring := auth.KeyRing{Keys: []auth.TrustedKey{platformTrust, {ID: signer.KeyID, PublicKey: string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), Issuer: "urn:fraud:service:ingestion-service", ServiceID: "ingestion-service", PrincipalTypes: []auth.PrincipalType{auth.PrincipalService}, Capabilities: []string{"models:read", "ingestion:events"}, Audiences: []string{"urn:fraud:data-model-service", "urn:fraud:decision-engine-service"}}}}
	dir := t.TempDir()
	ringData, err := json.Marshal(ring)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(signer.Key)
	if err != nil {
		t.Fatal(err)
	}
	trustFile, keyFile := filepath.Join(dir, "public.json"), filepath.Join(dir, "private.pem")
	if err := os.WriteFile(trustFile, ringData, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	config := db.Config().ConnConfig
	dsn := url.URL{Scheme: "postgres", Host: net.JoinHostPort(config.Host, strconv.Itoa(int(config.Port))), Path: "/" + config.Database, User: url.UserPassword(config.User, config.Password), RawQuery: "sslmode=disable"}
	command := exec.Command(binary)
	command.Dir = dir
	environment := map[string]string{"PORT": strconv.Itoa(port), "GIN_MODE": "release", "DATABASE_URL": dsn.String(), "DATA_MODEL_READ_MODE": "sql", "DATABASE_MAX_CONNS": "4", "PLATFORM_AUTH_KEYS_FILE": trustFile, "PLATFORM_AUTH_AUDIENCE": "urn:fraud:ingestion-service", "PLATFORM_AUTH_SIGNING_KEY_FILE": keyFile, "PLATFORM_AUTH_SIGNING_KEY_ID": signer.KeyID, "PLATFORM_AUTH_SERVICE_ID": signer.ServiceID}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if _, override := environment[name]; !override {
			command.Env = append(command.Env, entry)
		}
	}
	for name, value := range environment {
		command.Env = append(command.Env, name+"="+value)
	}
	log, err := os.Create(filepath.Join(dir, "ingestion.log"))
	if err != nil {
		t.Fatal(err)
	}
	command.Stdout = log
	command.Stderr = log
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	t.Cleanup(func() {
		_ = command.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = command.Process.Kill()
			<-done
		}
		log.Close()
	})
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	httpClient := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(10 * time.Second)
	for {
		response, err := httpClient.Get(base + "/healthz")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				break
			}
		}
		if time.Now().After(deadline) {
			data, _ := os.ReadFile(filepath.Join(dir, "ingestion.log"))
			t.Fatalf("ingestion startup failed: %s", data)
		}
		time.Sleep(50 * time.Millisecond)
	}
	proof := clientAssertion(t, key, "customer-key", client.ID, owner.TokenEndpointURL, uuid.NewString())
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {client.ID.String()}, "client_assertion_type": {"urn:ietf:params:oauth:client-assertion-type:jwt-bearer"}, "client_assertion": {proof}, "audience": {auth.ClientAudience}, "scope": {"ingest:write decisions:evaluate decisions:read"}}
	response, err := httpClient.PostForm(owner.TokenEndpointURL, form)
	if err != nil {
		t.Fatal(err)
	}
	var issued struct {
		Token   string `json:"access_token"`
		Expires int    `json:"expires_in"`
	}
	err = json.NewDecoder(response.Body).Decode(&issued)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || issued.Expires != 900 || issued.Token == "" {
		t.Fatal("proof exchange failed", response.StatusCode, err)
	}
	post := func(target uuid.UUID, want int) {
		t.Helper()
		request, err := http.NewRequest("POST", base+"/v1/tenants/"+target.String()+"/ingest/transactions", strings.NewReader(`{"object_id":"customer-transaction","amount":42}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+issued.Token)
		response, err := httpClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		if response.StatusCode != want {
			t.Fatalf("ingestion status %d want %d: %s", response.StatusCode, want, body)
		}
	}
	post(uuid.New(), 403)
	post(tenant, 200)
	var amount float64
	if err := db.QueryRow(ctx, `SELECT amount FROM `+pgx.Identifier{schema, "transactions"}.Sanitize()+` WHERE object_id='customer-transaction'`).Scan(&amount); err != nil || amount != 42 {
		t.Fatal("transaction not persisted", amount, err)
	}
	decisionSigner := authtest.ServiceSigner(t, "decision-engine-service")
	modelURL := authtest.StartAPI(t, modelBinary, db, "data-model-service", []auth.TrustedKey{authtest.ServiceTrust(t, decisionSigner)}, nil)
	decisionURL := authtest.StartAPIWithSigner(t, decisionBinary, db, decisionSigner, []auth.TrustedKey{platformTrust}, map[string]string{"DATA_MODEL_SERVICE_URL": modelURL, "INGESTION_SERVICE_URL": base, "TENANT_DATA_READ_MODE": "direct_db"})
	scenario, iteration := uuid.New(), uuid.New()
	execSQL(`INSERT INTO core.scenarios(id,tenant_id,name,trigger_object_type,created_at,updated_at) VALUES($1,$2,'Client decisions','transactions',now(),now())`, scenario, tenant)
	execSQL(`INSERT INTO core.scenario_iterations(id,scenario_id,tenant_id,version,status,trigger_formula,created_at) VALUES($1,$2,$3,1,'live','{"constant":true}',now())`, iteration, scenario, tenant)
	execSQL(`UPDATE core.scenarios SET live_iteration_id=$2 WHERE id=$1`, scenario, iteration)
	decisionRequest := func(method, path, token, body string, want int) {
		t.Helper()
		r, err := http.NewRequest(method, decisionURL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		response, err := httpClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		if response.StatusCode != want {
			t.Fatalf("decision %s status=%d want=%d: %s", path, response.StatusCode, want, data)
		}
	}
	// Exactly the same access token is used for ingestion, evaluation and reads.
	decisionRequest("POST", "/v1/tenants/"+tenant.String()+"/scenarios/"+scenario.String()+"/evaluate", issued.Token, `{"object_id":"customer-transaction","object_type":"transactions","fields":{"amount":42}}`, 200)
	var persistedDecisions int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM core.decisions WHERE tenant_id=$1 AND scenario_id=$2 AND object_id='customer-transaction'`, tenant, scenario).Scan(&persistedDecisions); err != nil || persistedDecisions != 1 {
		t.Fatal("decision request did not persist exactly one decision", persistedDecisions, err)
	}
	decisionRequest("GET", "/v1/tenants/"+tenant.String()+"/decisions?limit=20", issued.Token, "", 200)
	decisionRequest("GET", "/v1/tenants/"+uuid.NewString()+"/decisions", issued.Token, "", 403)
	decisionRequest("GET", "/internal/screening-authoring/tenants/"+tenant.String()+"/scenarios/"+scenario.String(), issued.Token, "", 403)
	limitedProof := clientAssertion(t, key, "customer-key", client.ID, owner.TokenEndpointURL, uuid.NewString())
	limited, err := owner.ExchangeClientProof(ctx, client.ID, limitedProof, auth.ClientAudience, []string{"ingest:write"})
	if err != nil {
		t.Fatal(err)
	}
	decisionRequest("GET", "/v1/tenants/"+tenant.String()+"/decisions", limited, "", 403)
	response, err = httpClient.PostForm(owner.TokenEndpointURL, form)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("proof replay accepted", response.StatusCode)
	}
}
