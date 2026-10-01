package postgres_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/httpapi"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/store/postgres"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresAuthorizationAndInvestigation(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, inbox, c := fixture(t, db)
	admin := adminContext(tenant)
	user := userContext(tenant, "analyst-1")
	private, err := s.CreateInbox(admin, service.CreateInboxInput{TenantID: tenant, Name: "Private"})
	if err != nil {
		t.Fatal(err)
	}
	privateCase, err := s.CreateCase(serviceContext(tenant), service.CreateCaseInput{TenantID: tenant, InboxID: private.ID, Name: "Private case"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := s.ListCases(user, tenant, casepkg.CaseFilters{AccessUserID: "admin"}, 100)
	if err != nil || len(listed) != 1 || listed[0].ID != c.ID {
		t.Fatalf("list permissions: %+v %v", listed, err)
	}
	inboxes, err := s.ListInboxes(user, tenant)
	if err != nil || len(inboxes) != 1 || inboxes[0].ID != inbox.ID {
		t.Fatalf("inbox visibility: %+v %v", inboxes, err)
	}
	if _, err := s.GetCase(user, tenant, privateCase.ID); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatalf("private case: %v", err)
	}
	if _, err := s.ListEvents(user, tenant, privateCase.ID, 100); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatalf("private events: %v", err)
	}
	if _, err := s.CreateComment(user, tenant, privateCase.ID, "no access", nil); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatalf("private comment: %v", err)
	}
	if _, err := s.UpdateCase(user, service.UpdateCaseInput{TenantID: tenant, CaseID: c.ID, InboxID: &private.ID}, nil); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatalf("private destination: %v", err)
	}
	if _, err := s.PutInboxUser(user, tenant, private.ID, "analyst-1", true); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatalf("member self-grant: %v", err)
	}
	if _, err := s.GetCase(context.Background(), tenant, c.ID); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatalf("anonymous: %v", err)
	}
	if _, err := s.GetCase(userContext(uuid.New(), "analyst-1"), tenant, c.ID); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatalf("other tenant: %v", err)
	}
	nonmember := "nonmember"
	if err := s.Assign(admin, tenant, c.ID, &nonmember, nil); !errors.Is(err, casepkg.ErrValidation) {
		t.Fatalf("ineligible assignment: %v", err)
	}
	boost := "new_decision"
	if _, err := s.UpdateCase(serviceContext(tenant), service.UpdateCaseInput{TenantID: tenant, CaseID: c.ID, BoostReason: &boost}, nil); err != nil {
		t.Fatal(err)
	}
	spoofed := "admin"
	event, err := s.CreateComment(user, tenant, c.ID, "investigated", &spoofed)
	if err != nil {
		t.Fatal(err)
	}
	if event.UserID == nil || *event.UserID != "analyst-1" {
		t.Fatalf("actor spoofing: %+v", event)
	}
	got, err := s.GetCase(user, tenant, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != casepkg.StatusInvestigating || got.AssignedTo == nil || *got.AssignedTo != "analyst-1" || got.BoostReason != nil || len(got.Contributors) != 1 {
		t.Fatalf("investigation effects: %+v", got)
	}
	if _, err := s.CreateComment(user, tenant, c.ID, "more evidence", nil); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetCase(user, tenant, c.ID)
	if err != nil || len(got.Contributors) != 1 {
		t.Fatalf("duplicate contributor: %+v %v", got.Contributors, err)
	}
	if err := s.RemoveInboxUser(admin, tenant, inbox.ID, "analyst-1"); !errors.Is(err, casepkg.ErrConflict) {
		t.Fatalf("removed active assignee: %v", err)
	}
	if err := s.Assign(admin, tenant, c.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveInboxUser(admin, tenant, inbox.ID, "analyst-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetCase(user, tenant, c.ID); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatalf("revocation did not take effect: %v", err)
	}
}

func TestPostgresContributorAndMembershipRollback(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, inbox, c := fixture(t, db)
	ctx := userContext(tenant, "analyst-1")
	beforeEvents, beforeOutbox := count(t, db, "case_events"), count(t, db, "outbox_events")
	failInserts(t, db, "case_contributors")
	if _, err := s.CreateComment(ctx, tenant, c.ID, "rollback", nil); err == nil {
		t.Fatal("contributor failure was ignored")
	}
	if count(t, db, "case_events") != beforeEvents || count(t, db, "outbox_events") != beforeOutbox {
		t.Fatal("comment or outbox escaped rollback")
	}
	got, err := s.GetCase(ctx, tenant, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != casepkg.StatusPending || got.AssignedTo != nil {
		t.Fatal("side effects escaped rollback")
	}
	_, err = db.Exec(context.Background(), `DROP TRIGGER fail_test_write ON case_manager.case_contributors`)
	if err != nil {
		t.Fatal(err)
	}
	failInserts(t, db, "outbox_events")
	if _, err := s.PutInboxUser(adminContext(tenant), tenant, inbox.ID, "new-member", true); err == nil {
		t.Fatal("membership outbox failure ignored")
	}
	users, err := s.ListInboxUsers(adminContext(tenant), tenant, inbox.ID)
	if err != nil || len(users) != 1 {
		t.Fatalf("membership escaped rollback: %+v %v", users, err)
	}
}

func seedScreening(t *testing.T, db *pgxpool.Pool, tenant, decision uuid.UUID) (uuid.UUID, string, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	screening, file := uuid.New(), uuid.New()
	match := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO screening.screenings(id,tenant_id,decision_id,provider,object_type,object_id,status,created_at,updated_at) VALUES($1,$2,$3,'test','customer','customer-1','completed',now(),now())`, screening, tenant, decision); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO screening.screening_matches(id,tenant_id,screening_id,entity_id,provider,status,name,created_at,updated_at) VALUES($1,$2,$3,'entity-1','test','pending','Match',now(),now())`, match, tenant, screening); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO screening.screening_files(id,tenant_id,screening_id,file_name,content_type,file_size,storage_key,uploaded_by,created_at) VALUES($1,$2,$3,'evidence.pdf','application/pdf',10,'verified/key','uploader',now())`, file, tenant, screening); err != nil {
		t.Fatal(err)
	}
	return screening, match, file
}

func TestPostgresSharedResourceOwnership(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, _, c := fixture(t, db)
	ctx := adminContext(tenant)
	other := uuid.New()
	seedTenant(t, db, other)
	decision := seedDecision(t, db, tenant)
	otherDecision := seedDecision(t, db, other)
	if _, err := s.AddDecision(ctx, service.AddDecisionInput{TenantID: tenant, CaseID: c.ID, DecisionID: otherDecision}, nil); err == nil {
		t.Fatal("cross-tenant decision attached")
	}
	if _, err := s.AddDecision(ctx, service.AddDecisionInput{TenantID: tenant, CaseID: c.ID, DecisionID: decision, ObjectID: "spoofed"}, nil); !errors.Is(err, casepkg.ErrValidation) {
		t.Fatalf("spoofed object accepted: %v", err)
	}
	link, err := s.AddDecision(ctx, service.AddDecisionInput{TenantID: tenant, CaseID: c.ID, DecisionID: decision}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if link.ObjectID != "customer-1" || link.ObjectType != "customer" || link.ScenarioID == nil {
		t.Fatalf("metadata not hydrated: %+v", link)
	}
	screening, match, file := seedScreening(t, db, tenant, decision)
	otherScreening, otherMatch, otherFile := seedScreening(t, db, other, otherDecision)
	if err := s.HandleScreeningReviewed(serviceContext(tenant), tenant, otherScreening, nil, otherMatch, "no_hit", nil); err == nil {
		t.Fatal("cross-tenant screening accepted")
	}
	if err := s.HandleScreeningReviewed(serviceContext(tenant), tenant, screening, nil, otherMatch, "no_hit", nil); err == nil {
		t.Fatal("wrong screening match accepted")
	}
	if err := s.HandleScreeningReviewed(serviceContext(tenant), tenant, screening, &otherDecision, match, "no_hit", nil); !errors.Is(err, casepkg.ErrValidation) {
		t.Fatalf("wrong linked decision: %v", err)
	}
	if _, err := s.AddFile(ctx, casepkg.File{TenantID: tenant, CaseID: c.ID, SourceFileID: &otherFile}, nil); err == nil {
		t.Fatal("cross-tenant file accepted")
	}
	if _, err := s.AddFile(ctx, casepkg.File{TenantID: tenant, CaseID: c.ID, StorageKey: "untrusted/key", FileName: "fake"}, nil); !errors.Is(err, casepkg.ErrValidation) {
		t.Fatalf("raw storage key accepted: %v", err)
	}
	attached, err := s.AddFile(ctx, casepkg.File{TenantID: tenant, CaseID: c.ID, SourceFileID: &file, StorageKey: "spoofed/key", FileName: "spoofed"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if attached.StorageKey != "verified/key" || attached.FileName != "evidence.pdf" {
		t.Fatalf("untrusted metadata persisted: %+v", attached)
	}
}

type queryCounter struct{ n atomic.Int64 }

func (q *queryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	q.n.Add(1)
	return ctx
}
func (q *queryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestPostgresBoundedQueueReadsAndScreeningLookup(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, inbox, c := fixture(t, db)
	ctx := adminContext(tenant)
	decision := seedDecision(t, db, tenant)
	if _, err := s.AddDecision(serviceContext(tenant), service.AddDecisionInput{TenantID: tenant, CaseID: c.ID, DecisionID: decision}, nil); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(time.Hour)
	if err := s.Snooze(ctx, tenant, c.ID, &until, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO case_manager.cases(id,tenant_id,inbox_id,name,status,outcome,type,created_at,updated_at)
	SELECT gen_random_uuid(),$1,$2,'Load case '||n,'pending','unset','decision',now(),now() FROM generate_series(1,1200) n`, tenant, inbox.ID); err != nil {
		t.Fatal(err)
	}
	screening, match, _ := seedScreening(t, db, tenant, decision)
	before := count(t, db, "cases")
	if err := s.HandleScreeningReviewed(serviceContext(tenant), tenant, screening, &decision, match, "no_hit", nil); err != nil {
		t.Fatal(err)
	}
	if count(t, db, "cases") != before {
		t.Fatal("lookup created a duplicate case")
	}
	got, err := s.GetCase(ctx, tenant, c.ID)
	if err != nil || len(got.Screenings) != 1 {
		t.Fatalf("snoozed case not located: %+v %v", got.Screenings, err)
	}
	// Populate real owned decisions and links so the lookup plan is measured
	// against a queue-sized relationship table, rather than a single fixture row.
	if _, err := db.Exec(ctx, `WITH added AS (
	INSERT INTO core.decisions(id,tenant_id,scenario_id,scenario_iteration_id,object_id,object_type,outcome,created_at)
	SELECT gen_random_uuid(),d.tenant_id,d.scenario_id,d.scenario_iteration_id,'load-'||n,'customer','review',now()
	FROM core.decisions d CROSS JOIN generate_series(1,1200) n WHERE d.id=$1 RETURNING id,tenant_id)
	INSERT INTO case_manager.case_decisions(id,tenant_id,case_id,decision_id,created_at)
	SELECT gen_random_uuid(),tenant_id,$2,id,now() FROM added`, decision, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `ANALYZE case_manager.cases; ANALYZE case_manager.case_decisions; ANALYZE case_manager.case_tags`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Fraud", "Review", "Priority"} {
		tag, err := s.CreateTag(ctx, service.CreateTagInput{TenantID: tenant, Name: name})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, `INSERT INTO case_manager.case_tags(id,tenant_id,case_id,tag_id,created_at)
		SELECT gen_random_uuid(),tenant_id,id,$2,now() FROM case_manager.cases WHERE tenant_id=$1`, tenant, tag.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(ctx, `ANALYZE case_manager.case_tags; ANALYZE case_manager.tags`); err != nil {
		t.Fatal(err)
	}
	counter := &queryCounter{}
	cfg := db.Config()
	cfg.ConnConfig.Tracer = counter
	measured, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer measured.Close()
	readService := newService(measured)
	for _, limit := range []int{10, 100, 500} {
		counter.n.Store(0)
		started := time.Now()
		items, err := readService.ListCases(userContext(tenant, "analyst-1"), tenant, casepkg.CaseFilters{}, limit)
		if err != nil || len(items) != limit {
			t.Fatalf("queue read: %d %v", len(items), err)
		}
		if counter.n.Load() != 2 {
			t.Fatalf("queue query count scales with rows: %d", counter.n.Load())
		}
		for _, item := range items {
			if len(item.Tags) != 3 {
				t.Fatalf("incomplete batch tag enrichment: %+v", item)
			}
		}
		t.Logf("queue rows=%d queries=%d elapsed=%s", limit, counter.n.Load(), time.Since(started))
	}
	rows, err := db.Query(ctx, `EXPLAIN (ANALYZE,BUFFERS) SELECT case_id FROM case_manager.case_decisions WHERE tenant_id=$1 AND decision_id=$2`, tenant, decision)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	t.Log("decision reference plan:\n" + strings.Join(plan, "\n"))
}

func TestPostgresSharedInboxLockConcurrency(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, inbox, _ := fixture(t, db)
	ctx, cancel := context.WithTimeout(adminContext(tenant), 5*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, `SELECT id FROM case_manager.inboxes WHERE tenant_id=$1 AND id=$2 FOR SHARE`, tenant, inbox.ID); err != nil {
		t.Fatal(err)
	}
	createDone := make(chan error, 1)
	go func() {
		_, err := s.CreateCase(ctx, service.CreateCaseInput{TenantID: tenant, InboxID: inbox.ID, Name: "Concurrent"}, nil)
		createDone <- err
	}()
	select {
	case err := <-createDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("independent case creation serialized on inbox validation")
	}
	updateDone := make(chan error, 1)
	archived := "archived"
	go func() {
		_, err := s.UpdateInbox(ctx, service.UpdateInboxInput{TenantID: tenant, InboxID: inbox.ID, Status: &archived})
		updateDone <- err
	}()
	select {
	case err := <-updateDone:
		t.Fatalf("archive bypassed shared lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-updateDone; err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateCase(ctx, service.CreateCaseInput{TenantID: tenant, InboxID: inbox.ID, Name: "Archived"}, nil); !errors.Is(err, casepkg.ErrValidation) {
		t.Fatalf("archived inbox accepted: %v", err)
	}
}

func TestPostgresSharedDatabasePoolAndLockBudgets(t *testing.T) {
	db := testDatabase(t, true)
	_, tenant, inbox, _ := fixture(t, db)
	t.Setenv("DB_POOL_MAX_CONNS", "2")
	t.Setenv("DB_LOCK_TIMEOUT", "100ms")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	databaseURL, err := url.Parse(db.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	databaseURL.Path = "/" + db.Config().ConnConfig.Database
	// Two independent process pools contend against the same database/schema.
	for replica := 0; replica < 2; replica++ {
		pool, err := store.NewPool(ctx, databaseURL.String())
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Close()
		leases := make([]*pgxpool.Conn, 2)
		for i := range leases {
			leases[i], err = pool.Acquire(ctx)
			if err != nil {
				t.Fatal(err)
			}
		}
		waitCtx, stop := context.WithTimeout(ctx, 50*time.Millisecond)
		_, err = pool.Acquire(waitCtx)
		stop()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("unbounded pool acquire: %v", err)
		}
		for _, conn := range leases {
			conn.Release()
		}
		stat := pool.Stat()
		if stat.TotalConns() > 2 || stat.CanceledAcquireCount() != 1 {
			t.Fatalf("pool limit not enforced: %+v", stat)
		}
		t.Logf("replica=%d connections=%d canceled_waits=%d acquire_duration=%s", replica, stat.TotalConns(), stat.CanceledAcquireCount(), stat.AcquireDuration())
		blocking, err := db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := blocking.Exec(ctx, `SELECT id FROM case_manager.inboxes WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, inbox.ID); err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		_, err = pool.Exec(ctx, `SELECT id FROM case_manager.inboxes WHERE tenant_id=$1 AND id=$2 FOR SHARE`, tenant, inbox.ID)
		_ = blocking.Rollback(ctx)
		var pgerr *pgconn.PgError
		if !errors.As(err, &pgerr) || pgerr.Code != "55P03" {
			t.Fatalf("lock budget not enforced: %v", err)
		}
		t.Logf("replica=%d lock_timeout_elapsed=%s", replica, time.Since(started))
	}
}

func TestPostgresHTTPIdentityBoundary(t *testing.T) {
	db := testDatabase(t, true)
	_, tenant, inbox, c := fixture(t, db)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	public := pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey)})
	verifier, err := access.NewVerifier(map[string][]byte{"test": public}, "issuer", "case-manager")
	if err != nil {
		t.Fatal(err)
	}
	sign := func(subject string, tenantID uuid.UUID) string {
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, access.Claims{
			RegisteredClaims: jwt.RegisteredClaims{Issuer: "issuer", Subject: subject, Audience: jwt.ClaimStrings{"case-manager"}, IssuedAt: jwt.NewNumericDate(time.Now()), ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
			TenantID:         tenantID.String(), Roles: []string{"case_investigator"},
		})
		token.Header["kid"] = "test"
		raw, err := token.SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	router := httpapi.NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), db, httpapi.RouterConfig{UserVerifier: verifier, AuthMode: "token", AuthToken: "service-token", ServiceTenantIDs: []uuid.UUID{tenant}})
	casePath := "/v1/tenants/" + tenant.String() + "/cases/" + c.ID.String()
	inboxPath := "/internal/v1/tenants/" + tenant.String() + "/inboxes/" + inbox.ID.String()
	for _, tt := range []struct {
		method, path, token, body string
		want                      int
	}{
		{"GET", casePath, sign("analyst-1", tenant), "", 200},
		{"GET", casePath, sign("outsider", tenant), "", 403},
		{"GET", casePath, sign("analyst-1", uuid.New()), "", 403},
		{"GET", casePath, "service-token", "", 401},
		{"POST", casePath + "/comments", sign("analyst-1", tenant), `{"comment":"Verified action"}`, 201},
		{"GET", inboxPath, "service-token", "", 200},
		{"GET", inboxPath, sign("analyst-1", tenant), "", 401},
		{"POST", "/internal/v1/auto-assignment/run", "service-token", "{}", 501},
	} {
		req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
		req.Header.Set("Authorization", "Bearer "+tt.token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Actor-ID", "spoofed-admin")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != tt.want {
			t.Fatalf("%s %s: want %d got %d: %s", tt.method, tt.path, tt.want, w.Code, w.Body.String())
		}
	}
	var actor string
	if err := db.QueryRow(context.Background(), `SELECT user_id FROM case_manager.case_events WHERE tenant_id=$1 AND case_id=$2 AND event_type='comment_added'`, tenant, c.ID).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if actor != "analyst-1" {
		t.Fatalf("unverified audit actor: %s", actor)
	}
}
