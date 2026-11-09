package localdelivery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/testsupport"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func providerFixture(t *testing.T, interpolation ...bool) (*Store, *Provider) {
	t.Helper()
	db, name := testsupport.DecisionMySQLDatabase(t)
	if len(interpolation) > 0 && interpolation[0] {
		var err error
		db, err = mysqlstore.Open(context.Background(), mysqlstore.Config{Address: os.Getenv("DECISION_MYSQL_TEST_ADDRESS"), Database: name, User: "root", Password: "isolated-test-root-only", AllowInsecureTransport: true, InterpolateParameters: true, MaxOpen: 8, MaxIdle: 4, ConnectTimeout: time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
	}
	schema, _ := mysqlstore.Identifier(name)
	tenant := uuid.New()
	// Declared consumer fixture for the tenant registry. The live Docker audit
	// installs and uses the full data-model, case and screening owner migrations.
	for _, q := range []string{"CREATE TABLE " + schema + ".data_model_tenants(id BINARY(16) PRIMARY KEY,status VARCHAR(16) NOT NULL)", "INSERT INTO " + schema + ".data_model_tenants VALUES(UUID_TO_BIN('" + tenant.String() + "'),'active')"} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	steps, err := Statements(name, "root")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range steps {
		if _, err := db.Exec(s.SQL); err != nil {
			t.Fatal(s.ID, err)
		}
	}
	authorize := func(ctx context.Context, tx *sql.Tx, id uuid.UUID, _ bool) error {
		if id != tenant {
			return ErrRequest
		}
		var active bool
		if err := tx.QueryRowContext(ctx, "SELECT status='active' FROM "+schema+".data_model_tenants WHERE id=?", mysqlstore.UUID(id)).Scan(&active); err != nil {
			return err
		}
		if !active {
			return ErrRequest
		}
		return nil
	}
	store, err := NewStore(db, name, tenant, authorize)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := NewProvider(db, name, strings.Repeat("a", 64), tenant, authorize)
	if err != nil {
		t.Fatal(err)
	}
	return store, provider
}

func TestLocalDeliveryInterpolatedJSONKeepsExactPayloadAndReceipt(t *testing.T) {
	s, _ := providerFixture(t, true)
	ctx := context.Background()
	r := testRequest(s.tenant, ports.JobCallback)
	r.Payload = json.RawMessage(" { \"unicode\":\"😀\", \"quote\":\"'\\\\\", \"nested\":null } ")
	hash := sha256.Sum256(r.Payload)
	r.PayloadSHA256 = hex.EncodeToString(hash[:])
	result := json.RawMessage(`{"precision":9007199254740993,"unicode":"😀"}`)
	first, err := s.Record(ctx, r, result)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.Record(ctx, r, result)
	if err != nil || first.Reference != again.Reference || first.Outcome != ports.DispatchAccepted {
		t.Fatal("duplicate receipt identity", first, again, err)
	}
	var payload, stored, storedHash []byte
	if err := s.db.QueryRowContext(ctx, "SELECT payload,result,payload_hash FROM "+s.schema+".decision_local_delivery_receipts WHERE tenant_id=? AND kind=? AND resource_id=?", mysqlstore.UUID(s.tenant), []byte(r.Kind), mysqlstore.UUID(uuid.MustParse(r.ResourceID))).Scan(&payload, &stored, &storedHash); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(payload, r.Payload) || !bytes.Equal(storedHash, hash[:]) {
		t.Fatal("binary archive or payload hash changed")
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(stored, &decoded); err != nil || string(decoded["precision"]) != "9007199254740993" {
		t.Fatal("JSON result lost precision", string(stored), err)
	}
	if receipt, err := s.Reconcile(ctx, r); err != nil || receipt.Reference != first.Reference || receipt.Outcome != ports.DispatchAccepted {
		t.Fatal("cannot reconcile durable acceptance", receipt, err)
	}
}
func testRequest(tenant uuid.UUID, kind ports.JobKind, resources ...uuid.UUID) ports.DispatchRequest {
	resource := uuid.New()
	if len(resources) > 0 {
		resource = resources[0]
	}
	id := mysqljobs.Identity{Version: 1, TenantID: tenant, ResourceID: resource, Kind: kind, DomainAttempt: 1}
	key, _ := id.Key()
	raw, _ := json.Marshal(key)
	hash := sha256.Sum256(raw)
	payload, _ := json.Marshal(map[string]any{"callback_url": CallbackURL, "body": map[string]any{"status": "completed"}})
	digest := sha256.Sum256(payload)
	return ports.DispatchRequest{Key: "decision-v1-" + hex.EncodeToString(hash[:]), TenantID: tenant.String(), ResourceID: id.ResourceID.String(), Kind: kind, DomainAttempt: 1, PayloadSHA256: hex.EncodeToString(digest[:]), Payload: payload}
}
func TestLocalDeliveryConcurrentReceiptsPayloadBindingAndRevocation(t *testing.T) {
	s, _ := providerFixture(t)
	ctx := context.Background()
	request := testRequest(s.tenant, ports.JobCallback)
	references := make(chan string, 16)
	errs := make(chan error, 16)
	var group sync.WaitGroup
	for range 16 {
		group.Add(1)
		go func() {
			defer group.Done()
			out, err := s.Record(ctx, request, json.RawMessage(`{"received":true}`))
			references <- out.Reference
			errs <- err
		}()
	}
	group.Wait()
	close(references)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	reference := ""
	for r := range references {
		if reference != "" && r != reference {
			t.Fatal("duplicate durable acceptance")
		}
		reference = r
	}
	changed := request
	changed.Payload = json.RawMessage(`{"different":true}`)
	hash := sha256.Sum256(changed.Payload)
	changed.PayloadSHA256 = hex.EncodeToString(hash[:])
	if _, err := s.Record(ctx, changed, json.RawMessage(`{}`)); !errors.Is(err, ErrConflict) {
		t.Fatal("rebound payload", err)
	}
	if out, err := s.Reconcile(ctx, request); err != nil || out.Reference != reference || out.Outcome != ports.DispatchAccepted {
		t.Fatal(out, err)
	}
	foreign := testRequest(uuid.New(), ports.JobCallback)
	if _, err := s.Reconcile(ctx, foreign); !errors.Is(err, ErrRequest) {
		t.Fatal("foreign tenant accepted", err)
	}
	if _, err := s.db.Exec("UPDATE "+s.schema+".data_model_tenants SET status='inactive' WHERE id=?", mysqlstore.UUID(s.tenant)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reconcile(ctx, request); err == nil {
		t.Fatal("inactive tenant retained access")
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestLocalDeliveryHTTPUnknownResponseReconcilesAndRejectsRedirects(t *testing.T) {
	s, p := providerFixture(t)
	server := httptest.NewServer(p.Handler())
	defer server.Close()
	client, err := NewClient(BaseURL, strings.Repeat("a", 64), s.tenant, ports.JobCallback)
	if err != nil {
		t.Fatal(err)
	}
	client.base = server.URL
	request := testRequest(s.tenant, ports.JobCallback)
	client.http.Transport = roundTripper(func(r *http.Request) (*http.Response, error) {
		res, err := http.DefaultTransport.RoundTrip(r)
		if err != nil {
			return nil, err
		}
		res.Body.Close()
		return nil, errors.New("isolated lost response after receiver commit")
	})
	if _, err := client.Deliver(context.Background(), request); err == nil {
		t.Fatal("lost response reported success")
	}
	client.http.Transport = nil
	receipt, err := client.Reconcile(context.Background(), request)
	if err != nil || receipt.Outcome != ports.DispatchAccepted {
		t.Fatal(receipt, err)
	}
	replay, err := client.Deliver(context.Background(), request)
	if err != nil || replay != receipt {
		t.Fatal("receipt replay changed", replay, err)
	}
	rows, err := s.Read(context.Background(), ports.JobCallback, uuid.MustParse(request.ResourceID))
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	probe := httptest.NewRecorder()
	p.Handler().ServeHTTP(probe, httptest.NewRequest("POST", "/internal/local-deliveries/deliver", strings.NewReader(`{}`)))
	if probe.Code != 401 {
		t.Fatal("unauthenticated receiver", probe.Code)
	}
	redirected := 0
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected++ }))
	defer destination.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 307) }))
	defer redirect.Close()
	client.base = redirect.URL
	if _, err := client.Deliver(context.Background(), request); err == nil || redirected != 0 {
		t.Fatal("redirect followed", redirected, err)
	}
}
func TestLocalDeliveryLateReceiptFailureHasNoAcceptance(t *testing.T) {
	s, _ := providerFixture(t)
	if _, err := s.db.Exec("CREATE TRIGGER " + s.schema + ".fixture_receipt_failure BEFORE INSERT ON " + s.schema + ".decision_local_delivery_receipts FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='isolated receipt failure'"); err != nil {
		t.Fatal(err)
	}
	request := testRequest(s.tenant, ports.JobCallback)
	if out, err := s.Record(context.Background(), request, json.RawMessage(`{}`)); err == nil || out.Outcome == ports.DispatchAccepted {
		t.Fatal("uncommitted acceptance", out, err)
	}
	out, err := s.Reconcile(context.Background(), request)
	if err != nil || out.Outcome != ports.DispatchNotFound {
		t.Fatal(out, err)
	}
}

func TestLocalDeliveryPayloadBudgetMatchesExecutionOwner(t *testing.T) {
	s, p := providerFixture(t)
	server := httptest.NewServer(p.Handler())
	defer server.Close()
	client, err := NewClient(BaseURL, strings.Repeat("a", 64), s.tenant, ports.JobCallback)
	if err != nil {
		t.Fatal(err)
	}
	client.base = server.URL
	r := testRequest(s.tenant, ports.JobCallback)
	r.Payload, _ = json.Marshal(map[string]any{"callback_url": CallbackURL, "body": map[string]any{"padding": strings.Repeat("x", 2<<20)}})
	hash := sha256.Sum256(r.Payload)
	r.PayloadSHA256 = hex.EncodeToString(hash[:])
	if receipt, err := client.Deliver(context.Background(), r); err != nil || receipt.Outcome != ports.DispatchAccepted {
		t.Fatal(receipt, err)
	}
	saved, err := s.Read(context.Background(), ports.JobCallback, uuid.MustParse(r.ResourceID))
	if err != nil || len(saved) != 1 || string(saved[0].Request.Payload) != string(r.Payload) {
		t.Fatal("payload not durably retained", len(saved), err)
	}
	r.Payload = append(r.Payload, make([]byte, MaxPayloadBytes)...)
	if err := Validate(r, s.tenant); err == nil {
		t.Fatal("accepted oversized payload")
	}
}
