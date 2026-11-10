package integrationtest

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/mysqlidentity"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	c "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/ports"
	store "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/mysqlcase"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/mysqlreview"
	dm "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/mysqlschema"
	de "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/mysqlschema"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	screening "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/mysqlschema"
	"github.com/google/uuid"
)

type caseFixture struct {
	db                              *sql.DB
	reference                       *sql.DB
	screeningPlan                   migrations.Plan
	database                        string
	tenant, other, user, sid, inbox uuid.UUID
	ctx                             context.Context
}

func fixture(t *testing.T) *caseFixture {
	t.Helper()
	address, refAddress := os.Getenv("CASE_MYSQL_TEST_ADDRESS"), os.Getenv("CASE_MYSQL_REFERENCE_ADDRESS")
	if address == "" || refAddress == "" {
		if os.Getenv("CASE_REQUIRE_MYSQL_TEST") == "1" {
			t.Fatal("private target and reference required")
		}
		t.Skip("private MySQL fixtures not configured")
	}
	if address == refAddress {
		t.Fatal("independent servers required")
	}
	ctx := context.Background()
	password := os.Getenv("CASE_MYSQL_TEST_PASSWORD")
	if password == "" {
		password = "isolated-case-test-root"
	}
	database := "case_fixture_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	open := func(address, database string) *sql.DB {
		db, e := mysqlstore.Open(ctx, mysqlstore.Config{Address: address, Database: database, User: "root", Password: password, AllowInsecureTransport: true, MaxOpen: 8, MaxIdle: 4, ConnectTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute, LockWaitTimeout: 5 * time.Second})
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { db.Close() })
		return db
	}
	targetAdmin, referenceAdmin := open(address, "mysql"), open(refAddress, "mysql")
	var targetID, referenceID string
	must(t, targetAdmin.QueryRowContext(ctx, "SELECT @@server_uuid").Scan(&targetID))
	must(t, referenceAdmin.QueryRowContext(ctx, "SELECT @@server_uuid").Scan(&referenceID))
	if targetID == referenceID {
		t.Fatal("independent server UUIDs required")
	}
	for _, db := range []*sql.DB{targetAdmin, referenceAdmin} {
		_, e := db.ExecContext(ctx, "CREATE DATABASE `"+database+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin")
		must(t, e)
	}
	t.Cleanup(func() {
		for _, db := range []*sql.DB{targetAdmin, referenceAdmin} {
			_, e := db.ExecContext(context.Background(), "DROP DATABASE `"+database+"`")
			if e != nil {
				t.Error(e)
			}
		}
	})
	target, reference := open(address, database), open(refAddress, database)
	var screeningPlan migrations.Plan
	for _, owner := range []struct {
		name, guard string
		steps       func(string, string) ([]migrations.Step, error)
		grants      func(string, string) ([]string, error)
	}{
		{"data_model", "cm_test_model_guard", dm.Statements, dm.DefinerGrants}, {"decision_engine", "cm_test_decision_guard", de.Statements, de.DefinerGrants},
		{"identity", "cm_test_identity_guard", mysqlidentity.Statements, mysqlidentity.DefinerGrants}, {"screening", "cm_test_screening_guard", screening.Statements, screening.DefinerGrants},
		{"case_manager", "cm_test_case_guard", mysqlcase.Statements, mysqlcase.DefinerGrants},
	} {
		for _, db := range []*sql.DB{target, reference} {
			_, e := db.ExecContext(ctx, "CREATE USER IF NOT EXISTS `"+owner.guard+"`@`localhost` ACCOUNT LOCK")
			must(t, e)
		}
		steps, e := owner.steps(database, owner.guard)
		must(t, e)
		if owner.name == "data_model" {
			lookup, e := dm.RecordLookupStatements(database, owner.guard)
			must(t, e)
			steps = append(steps, lookup...)
		}
		conn, e := reference.Conn(ctx)
		must(t, e)
		for i := range steps {
			s := &steps[i]
			if strings.HasPrefix(s.SQL, "ALTER ") {
				s.Before, e = migrations.Definition(ctx, conn, database, s.Kind, s.Name)
				must(t, e)
			}
			if _, e = conn.ExecContext(ctx, s.SQL); e != nil {
				conn.Close()
				t.Fatalf("reference %s/%s: %v", owner.name, s.ID, e)
			}
			s.After, e = migrations.Definition(ctx, conn, database, s.Kind, s.Name)
			must(t, e)
		}
		conn.Close()
		plan := migrations.Plan{Database: database, Owner: owner.name, Steps: steps}
		if owner.name == "screening" {
			screeningPlan = plan
		}
		var legacyCase, legacyMember uuid.UUID
		if owner.name == "case_manager" {
			prefix := plan
			for n, s := range steps {
				if strings.HasPrefix(s.ID, "case_lifecycle_") {
					prefix.Steps = steps[:n]
					break
				}
			}
			old, e := migrations.NewForOwner(target, owner.name, prefix)
			must(t, e)
			must(t, old.Apply(ctx))
			oldGrants, e := mysqlreview.DefinerGrants(database, owner.guard)
			must(t, e)
			for _, q := range oldGrants {
				_, e = target.ExecContext(ctx, q)
				must(t, e)
			}
			tenant, inbox := uuid.New(), uuid.New()
			legacyCase = uuid.New()
			legacyMember = uuid.New()
			for _, v := range []struct {
				q string
				a []any
			}{
				{"INSERT INTO `" + database + "`.identity_tenants VALUES(?,TRUE)", []any{id(tenant)}},
				{"INSERT INTO `" + database + "`.identity_users VALUES(?,'Existing workflow member',TRUE)", []any{id(legacyMember)}},
				{"INSERT INTO `" + database + "`.identity_memberships(tenant_id,user_id,roles,active,version) VALUES(?,?,'case_investigator',TRUE,1)", []any{id(tenant), id(legacyMember)}},
				{"INSERT INTO `" + database + "`.case_manager_inboxes(id,tenant_id,name,active) VALUES(?,?,'Existing workflow inbox',TRUE)", []any{id(inbox), id(tenant)}},
				{"INSERT INTO `" + database + "`.case_manager_inbox_users(tenant_id,inbox_id,user_id) VALUES(?,?,?)", []any{id(tenant), id(inbox), id(legacyMember)}},
				{"INSERT INTO `" + database + "`.case_manager_cases(id,tenant_id,inbox_id,name,status,outcome,type,created_at,updated_at) VALUES(?,?,?,'Existing workflow case','pending','unset','decision',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))", []any{id(legacyCase), id(tenant), id(inbox)}},
				{"INSERT INTO `" + database + "`.case_manager_intake_receipts(tenant_id,source,event_id,payload_hash,case_id,created_at) VALUES(?,'upgrade-compatibility',?,UNHEX(REPEAT('ab',32)),?,UTC_TIMESTAMP(6))", []any{id(tenant), id(uuid.New()), id(legacyCase)}},
			} {
				_, e = target.ExecContext(ctx, v.q, v.a...)
				must(t, e)
			}
		}
		runner, e := migrations.NewForOwner(target, owner.name, plan)
		must(t, e)
		must(t, runner.Apply(ctx))
		if legacyCase != uuid.Nil {
			var memberID mysqlstore.UUID
			must(t, target.QueryRowContext(ctx, "SELECT id FROM `"+database+"`.case_manager_inbox_users WHERE user_id=?", id(legacyMember)).Scan(&memberID))
			if uuid.UUID(memberID) == uuid.Nil {
				t.Fatal("existing inbox member has no persistent row ID")
			}
			var name, status string
			must(t, target.QueryRowContext(ctx, "SELECT name,status FROM `"+database+"`.case_manager_cases WHERE id=?", id(legacyCase)).Scan(&name, &status))
			if name != "Existing workflow case" || status != "pending" {
				t.Fatal("existing workflow case changed during upgrade")
			}
			var n int
			must(t, target.QueryRowContext(ctx, "SELECT COUNT(*) FROM `"+database+"`.case_manager_intake_receipts WHERE case_id=? AND payload_hash=UNHEX(REPEAT('ab',32))", id(legacyCase)).Scan(&n))
			if n != 1 {
				t.Fatal("existing workflow receipt changed during upgrade")
			}
		}
		must(t, runner.Apply(ctx))
		grants, e := owner.grants(database, owner.guard)
		must(t, e)
		if owner.name == "data_model" {
			grants = append(grants, "GRANT EXECUTE ON FUNCTION `"+database+"`.data_model_find_record_object_key_v1 TO `"+owner.guard+"`@`localhost`")
		}
		for _, db := range []*sql.DB{reference, target} {
			for _, q := range grants {
				_, e = db.ExecContext(ctx, q)
				must(t, e)
			}
		}
	}
	f := &caseFixture{db: target, reference: reference, screeningPlan: screeningPlan, database: database, tenant: uuid.New(), other: uuid.New(), user: uuid.New(), sid: uuid.New(), inbox: uuid.New()}
	for _, tenant := range []uuid.UUID{f.tenant, f.other} {
		_, e := target.ExecContext(ctx, "INSERT INTO `"+database+"`.identity_tenants VALUES(?,TRUE)", id(tenant))
		must(t, e)
	}
	_, e := target.ExecContext(ctx, "INSERT INTO `"+database+"`.identity_users VALUES(?,'Case tester',TRUE)", id(f.user))
	must(t, e)
	_, e = target.ExecContext(ctx, "INSERT INTO `"+database+"`.identity_memberships(tenant_id,user_id,roles,active,version) VALUES(?,?,'case_manager_admin',TRUE,1)", id(f.tenant), id(f.user))
	must(t, e)
	digest := make([]byte, 32)
	copy(digest, f.sid[:])
	_, e = target.ExecContext(ctx, "INSERT INTO `"+database+"`.identity_sessions(id,digest,tenant_id,user_id,created_at,expires_at,idle_until) VALUES(?,?,?,?,UTC_TIMESTAMP(6),DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 1 HOUR),DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 1 HOUR))", id(f.sid), digest, id(f.tenant), id(f.user))
	must(t, e)
	f.ctx = access.WithPrincipal(ctx, access.Principal{Kind: access.User, Subject: f.user.String(), TenantID: f.tenant, Admin: true, SessionID: f.sid, AuthzVersion: 1})
	repos, e := store.NewRepositories(target, database)
	must(t, e)
	now := time.Now().UTC()
	_, e = repos.Inboxes.Create(ctx, c.Inbox{ID: f.inbox, TenantID: f.tenant, Name: "Fraud investigations", Status: "active", CreatedAt: now, UpdatedAt: now})
	must(t, e)
	return f
}
func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func (f *caseFixture) uow() store.UnitOfWork { return store.UnitOfWork{DB: f.db, Database: f.database} }
func (f *caseFixture) newCase(t *testing.T, name string) c.Case {
	t.Helper()
	var v c.Case
	must(t, f.uow().WithinTransaction(f.ctx, func(r ports.Repositories) error {
		now := time.Now().UTC()
		var e error
		v, e = r.Cases.Create(f.ctx, c.Case{ID: uuid.New(), TenantID: f.tenant, InboxID: f.inbox, Name: name, Type: c.TypeDecision, Status: c.StatusPending, Outcome: c.OutcomeUnset, CreatedAt: now, UpdatedAt: now})
		return e
	}))
	return v
}

func TestMySQLCaseLifecycle(t *testing.T) {
	f := fixture(t)
	v := f.newCase(t, "Investigate large transfer")
	repos, e := store.NewRepositories(f.db, f.database)
	must(t, e)
	t.Run("tenant isolation", func(t *testing.T) {
		_, e := repos.Cases.Get(f.ctx, f.other, v.ID)
		if !errors.Is(e, c.ErrNotFound) {
			t.Fatalf("cross tenant: %v", e)
		}
	})
	t.Run("queue name searches use literal wildcard characters", func(t *testing.T) {
		literal := f.newCase(t, "Transfer 100%_verified")
		f.newCase(t, "Transfer 100XXverified")
		page, e := repos.Workspace.Queue(f.ctx, f.tenant, c.WorkspaceFilters{CaseFilters: c.CaseFilters{Name: "%_", IncludeSnoozed: true}}, 10)
		must(t, e)
		if len(page.Cases) != 1 || page.Cases[0].ID != literal.ID {
			t.Fatalf("wildcard characters broadened queue selection: %+v", page.Cases)
		}
	})
	t.Run("case event and outbox rollback", func(t *testing.T) {
		event := uuid.New()
		sentinel := errors.New("abort")
		e := f.uow().WithinTransaction(f.ctx, func(r ports.Repositories) error {
			_, e := r.Events.Create(f.ctx, c.Event{ID: event, TenantID: f.tenant, CaseID: v.ID, EventType: "comment_added", AdditionalNote: "review source", CreatedAt: time.Now().UTC()})
			if e != nil {
				return e
			}
			return sentinel
		})
		if !errors.Is(e, sentinel) {
			t.Fatal(e)
		}
		for _, table := range []string{"case_events", "outbox_events"} {
			var n int
			must(t, f.db.QueryRow("SELECT COUNT(*) FROM `"+f.database+"`.case_manager_"+table+" WHERE id=?", id(event)).Scan(&n))
			if n != 0 {
				t.Fatal("orphan", table)
			}
		}
	})
	t.Run("events and delivery fencing", func(t *testing.T) {
		must(t, f.uow().WithinTransaction(f.ctx, func(r ports.Repositories) error {
			_, e := r.Events.Create(f.ctx, c.Event{ID: uuid.New(), TenantID: f.tenant, CaseID: v.ID, EventType: "comment_added", AdditionalNote: "review source", CreatedAt: time.Now().UTC()})
			return e
		}))
		events, e := repos.Events.ListByCase(f.ctx, f.tenant, v.ID, 100)
		must(t, e)
		if len(events) != 1 || events[0].AdditionalNote != "review source" {
			t.Fatalf("events: %+v", events)
		}
		m := store.Maintenance{DB: f.db, Database: f.database}
		d, e := m.ClaimDelivery(f.ctx)
		must(t, e)
		if d == nil {
			t.Fatal("missing delivery")
		}
		next, e := m.ClaimDelivery(f.ctx)
		must(t, e)
		if next != nil {
			t.Fatal("double claim")
		}
		wrong := *d
		wrong.Token = uuid.New()
		if !errors.Is(m.FinishDelivery(f.ctx, wrong, ""), c.ErrConflict) {
			t.Fatal("stale lease accepted")
		}
		must(t, m.FinishDelivery(f.ctx, *d, ""))
		if !errors.Is(m.FinishDelivery(f.ctx, *d, ""), c.ErrConflict) {
			t.Fatal("duplicate completion accepted")
		}
		must(t, m.Replay(f.ctx, f.tenant, d.ID, false))
	})
	t.Run("tags preserve identity on reattachment", func(t *testing.T) {
		var tag c.Tag
		must(t, f.uow().WithinTransaction(f.ctx, func(r ports.Repositories) error {
			now := time.Now().UTC()
			var e error
			tag, e = r.Tags.Create(f.ctx, c.Tag{ID: uuid.New(), TenantID: f.tenant, Target: "case", Name: "High amount", Color: "red", CreatedAt: now, UpdatedAt: now})
			if e != nil {
				return e
			}
			if e = r.Tags.AddToCase(f.ctx, f.tenant, v.ID, tag.ID, uuid.New(), now); e != nil {
				return e
			}
			if e = r.Tags.RemoveFromCase(f.ctx, f.tenant, v.ID, tag.ID, now); e != nil {
				return e
			}
			return r.Tags.AddToCase(f.ctx, f.tenant, v.ID, tag.ID, uuid.New(), now)
		}))
		tags, e := repos.Tags.ListByCase(f.ctx, f.tenant, v.ID)
		must(t, e)
		if len(tags) != 1 || tags[0].ID != tag.ID {
			t.Fatal(tags)
		}
		must(t, repos.Tags.RemoveFromCase(f.ctx, f.tenant, v.ID, tag.ID, time.Now().UTC()))
		_, e = f.db.ExecContext(f.ctx, "INSERT INTO `"+f.database+"`.case_manager_case_tags(tenant_id,case_id,tag_id,created_at) VALUES(?,?,?,UTC_TIMESTAMP(6)) ON DUPLICATE KEY UPDATE tag_id=tag_id", id(f.tenant), id(v.ID), id(tag.ID))
		must(t, e)
		tags, e = repos.Tags.ListByCase(f.ctx, f.tenant, v.ID)
		must(t, e)
		if len(tags) != 1 {
			t.Fatal("published v1 workflow upsert did not restore the removed tag")
		}
	})
	t.Run("report optimistic version", func(t *testing.T) {
		now := time.Now().UTC()
		report := c.Report{ID: uuid.New(), TenantID: f.tenant, CaseID: v.ID, Status: "draft", Version: 1, CreatedBy: f.user.String(), CreatedAt: now, UpdatedAt: now, Payload: c.ReportPayload{Format: "internal_sar_v1", Content: c.ReportContent{Title: "Suspicious transfer"}}}
		must(t, repos.Reports.Create(f.ctx, report))
		report.Version = 2
		must(t, repos.Reports.Save(f.ctx, report, 1))
		if !errors.Is(repos.Reports.Save(f.ctx, report, 1), c.ErrConflict) {
			t.Fatal("stale version accepted")
		}
		got, e := repos.Reports.Get(f.ctx, f.tenant, v.ID, report.ID, false)
		must(t, e)
		if got.Version != 2 || got.Payload.Content.Title != "Suspicious transfer" {
			t.Fatal(got)
		}
	})
	t.Run("capacity serializes concurrent assignments", func(t *testing.T) {
		_, e := repos.Memberships.Put(f.ctx, c.InboxUser{ID: uuid.New(), TenantID: f.tenant, InboxID: f.inbox, UserID: f.user.String(), Capacity: 1, AutoAssignEnabled: true, CreatedAt: time.Now().UTC()})
		must(t, e)
		cases := []c.Case{f.newCase(t, "One"), f.newCase(t, "Two")}
		start := make(chan struct{})
		results := make(chan error, 2)
		var wg sync.WaitGroup
		for _, item := range cases {
			wg.Add(1)
			go func(v c.Case) {
				defer wg.Done()
				<-start
				results <- f.uow().WithinTransaction(f.ctx, func(r ports.Repositories) error {
					if _, e := r.Cases.Lock(f.ctx, f.tenant, v.ID); e != nil {
						return e
					}
					user := f.user.String()
					return r.Cases.Assign(f.ctx, f.tenant, v.ID, &user, time.Now().UTC())
				})
			}(item)
		}
		close(start)
		wg.Wait()
		close(results)
		success, conflict := 0, 0
		for e := range results {
			if e == nil {
				success++
			} else if errors.Is(e, c.ErrConflict) {
				conflict++
			} else {
				t.Fatal(e)
			}
		}
		if success != 1 || conflict != 1 {
			t.Fatalf("success=%d conflict=%d", success, conflict)
		}
	})
	t.Run("UUID spelling cannot bypass assignment capacity", func(t *testing.T) {
		upper := strings.ToUpper(f.user.String())
		e := f.uow().WithinTransaction(f.ctx, func(r ports.Repositories) error {
			return r.Cases.Assign(f.ctx, f.tenant, v.ID, &upper, time.Now().UTC())
		})
		if !errors.Is(e, c.ErrConflict) {
			t.Fatalf("noncanonical capacity bypass: %v", e)
		}
	})
	t.Run("large relationships require pagination", func(t *testing.T) {
		item := f.newCase(t, "Large evidence history")
		var values []string
		var a []any
		for range 501 {
			values = append(values, "(?,?,?,?,?,?,?,?,?)")
			key := uuid.New()
			a = append(a, id(key), id(f.tenant), id(item.ID), "evidence.txt", "text/plain", 1, "fixture-only/"+key.String(), f.user.String(), time.Now().UTC())
		}
		_, e := f.db.ExecContext(f.ctx, "INSERT INTO `"+f.database+"`.case_manager_case_files(id,tenant_id,case_id,file_name,content_type,file_size,storage_key,uploaded_by,created_at) VALUES"+strings.Join(values, ","), a...)
		must(t, e)
		_, e = repos.Files.ListByCase(f.ctx, f.tenant, item.ID)
		if !errors.Is(e, c.ErrValidation) {
			t.Fatalf("silently truncated evidence history: %v", e)
		}
		page, e := repos.Workspace.Links(f.ctx, f.tenant, item.ID, "files", nil, 100)
		must(t, e)
		if len(page.Items) != 100 || page.NextCursor == "" {
			t.Fatal("missing relationship pagination")
		}
	})
	t.Run("revocation enforced within transaction", func(t *testing.T) {
		_, e := f.db.Exec("UPDATE `"+f.database+"`.identity_sessions SET revoked_at=UTC_TIMESTAMP(6) WHERE id=?", id(f.sid))
		must(t, e)
		called := false
		e = f.uow().WithinTransaction(f.ctx, func(ports.Repositories) error { called = true; return nil })
		if !errors.Is(e, c.ErrForbidden) || called {
			t.Fatalf("revoked session accepted: %v callback=%v", e, called)
		}
	})
}

func id(v uuid.UUID) mysqlstore.UUID    { return mysqlstore.UUID(v) }
func uid(v *uuid.UUID) *mysqlstore.UUID { return (*mysqlstore.UUID)(v) }
