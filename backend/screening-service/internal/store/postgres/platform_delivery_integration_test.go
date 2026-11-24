package postgres_test

import (
	"context"
	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	authtest "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth/authtest"
	caseclient "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/postgres"
	"github.com/google/uuid"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSignedOutboxDeliveryToRealCaseAPI(t *testing.T) {
	binary := os.Getenv("CASE_MANAGER_INTEGRATION_BINARY")
	if binary == "" {
		t.Skip("fresh CASE_MANAGER_INTEGRATION_BINARY required for process acceptance")
	}
	db := authtest.SharedDatabase(t)
	ctx := context.Background()
	tenant, inbox, caseID, screening, event, actor := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	match := uuid.NewString()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'Delivery',$2,'active',now(),now())`, tenant, "t_"+tenant.String()[:8])
	exec(`INSERT INTO case_manager.inboxes(id,tenant_id,name,created_at,updated_at) VALUES($1,$2,'Delivery',now(),now())`, inbox, tenant)
	exec(`INSERT INTO case_manager.cases(id,tenant_id,inbox_id,name,status,outcome,type,created_at,updated_at) VALUES($1,$2,$3,'Delivery','investigating','unset','manual_screening',now(),now())`, caseID, tenant, inbox)
	exec(`INSERT INTO screening.screenings(id,tenant_id,provider,object_type,object_id,status,created_at,updated_at) VALUES($1,$2,'test','customer','one','reviewed',now(),now())`, screening, tenant)
	exec(`INSERT INTO screening.screening_matches(id,tenant_id,screening_id,entity_id,provider,status,name,created_at,updated_at) VALUES($1,$2,$3,'entity','test','no_hit','Match',now(),now())`, match, tenant, screening)
	exec(`INSERT INTO case_manager.case_screenings(id,tenant_id,case_id,screening_id,match_id,status,created_at) VALUES($1,$2,$3,$4,$5,'pending',now())`, uuid.New(), tenant, caseID, screening, match)
	signer := authtest.ServiceSigner(t, "screening-case-delivery-worker")
	verifier := authtest.ServiceVerifier(t, signer, "urn:fraud:case-manager-service")
	target := authtest.StartAPI(t, binary, db, "case-manager-service", []auth.TrustedKey{authtest.ServiceTrust(t, signer)}, nil)
	var mu sync.Mutex
	tokens := []string{}
	attempts := 0
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		p, err := verifier.Verify(raw)
		if err != nil || p.Subject != "screening-case-delivery-worker" || p.TenantID != tenant || p.OrigID != actor.String() || p.OrigType != auth.PrincipalUser {
			t.Error("worker identity or persisted provenance changed", err)
			w.WriteHeader(401)
			return
		}
		mu.Lock()
		tokens = append(tokens, raw)
		attempts++
		attempt := attempts
		mu.Unlock()
		request, err := http.NewRequestWithContext(r.Context(), r.Method, target+r.URL.Path, r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		request.Header = r.Header.Clone()
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Error(err)
			w.WriteHeader(502)
			return
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		if response.StatusCode != 204 {
			t.Errorf("real case callback failed %d: %s", response.StatusCode, body)
			w.WriteHeader(response.StatusCode)
			return
		}
		if attempt == 1 {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(204)
	}))
	defer proxy.Close()
	repo := store.NewCaseEventRepository(db)
	cmd := ports.ScreeningReviewedCommand{EventID: event.String(), TenantID: tenant.String(), ScreeningID: screening.String(), MatchID: match, Status: "no_hit"}
	origin := auth.WithTestPrincipal(ctx, auth.Principal{Type: auth.PrincipalUser, Subject: actor.String(), TenantID: tenant})
	if err := repo.Enqueue(origin, event.String(), tenant.String(), "reviewed", cmd); err != nil {
		t.Fatal(err)
	}
	publisher := caseclient.NewHTTPClient(proxy.URL, time.Second).WithSigner(signer)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for range 2 {
		if err := service.NewCaseDeliveryService(repo, publisher, logger).RunBatch(ctx, 1); err != nil {
			t.Fatal(err)
		}
		exec(`UPDATE screening.case_event_outbox SET available_at=now() WHERE id=$1`, event)
	}
	var status string
	var count int
	if err := db.QueryRow(ctx, `SELECT status,attempts FROM screening.case_event_outbox WHERE id=$1`, event).Scan(&status, &count); err != nil || status != "delivered" || count != 2 {
		t.Fatal(status, count, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(tokens) != 2 || tokens[0] == tokens[1] {
		t.Fatal("retry reused an expiring bearer token")
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM case_manager.intake_receipts WHERE tenant_id=$1 AND event_id=$2`, tenant, event).Scan(&count); err != nil || count != 1 {
		t.Fatal("case receiver did not deduplicate lost acknowledgement", count, err)
	}
}
