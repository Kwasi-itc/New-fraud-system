package integrationtest

import (
	"errors"
	"testing"
	"time"

	c "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/ports"
	store "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/store/mysql"
	"github.com/google/uuid"
)

func TestMySQLCaseWorkers(t *testing.T) {
	f := fixture(t)
	repos, e := store.NewRepositories(f.db, f.database)
	must(t, e)
	m := store.Maintenance{DB: f.db, Database: f.database}
	one, two := f.newCase(t, "Wake then assign"), f.newCase(t, "Remain queued")
	t.Run("expired unfinished uploads only", func(t *testing.T) {
		now := time.Now().UTC()
		expired := uuid.New()
		must(t, repos.Uploads.Create(f.ctx, c.Upload{File: c.File{ID: expired, TenantID: f.tenant, CaseID: one.ID, UploadedBy: f.user.String(), FileName: "expired.txt", ContentType: "text/plain", FileSize: 3, CreatedAt: now.Add(-2 * time.Hour)}, ExpiresAt: now.Add(-time.Hour)}))
		must(t, m.CleanUploads(f.ctx, 100))
		_, e := repos.Uploads.Lock(f.ctx, f.tenant, one.ID, expired)
		if !errors.Is(e, c.ErrNotFound) {
			t.Fatalf("expired upload remains: %v", e)
		}
	})
	t.Run("wake emits one event and outbox", func(t *testing.T) {
		past := time.Now().UTC().Add(-time.Minute)
		must(t, repos.Cases.Snooze(f.ctx, f.tenant, one.ID, &past, time.Now().UTC()))
		n, e := m.WakeSnoozed(f.ctx, 100)
		must(t, e)
		if n != 1 {
			t.Fatalf("woke=%d", n)
		}
		n, e = m.WakeSnoozed(f.ctx, 100)
		must(t, e)
		if n != 0 {
			t.Fatal("duplicate wake")
		}
		events, e := repos.Events.ListByCase(f.ctx, f.tenant, one.ID, 100)
		must(t, e)
		if len(events) != 1 || events[0].EventType != "case_unsnoozed" {
			t.Fatal(events)
		}
	})
	t.Run("automatic assignment respects capacity", func(t *testing.T) {
		inbox, e := repos.Inboxes.Get(f.ctx, f.tenant, f.inbox)
		must(t, e)
		inbox.AutoAssignEnabled = true
		_, e = repos.Inboxes.Update(f.ctx, inbox)
		must(t, e)
		_, e = repos.Memberships.Put(f.ctx, c.InboxUser{ID: uuid.New(), TenantID: f.tenant, InboxID: f.inbox, UserID: f.user.String(), Capacity: 1, AutoAssignEnabled: true, CreatedAt: time.Now().UTC()})
		must(t, e)
		inactiveUser, otherInbox := uuid.New(), uuid.New()
		now := time.Now().UTC()
		_, e = f.db.ExecContext(f.ctx, "INSERT INTO `"+f.database+"`.identity_users VALUES(?,'Suspended investigator',TRUE)", id(inactiveUser))
		must(t, e)
		_, e = f.db.ExecContext(f.ctx, "INSERT INTO `"+f.database+"`.identity_memberships(tenant_id,user_id,roles,active,version) VALUES(?,?,'case_investigator',TRUE,1)", id(f.tenant), id(inactiveUser))
		must(t, e)
		_, e = repos.Inboxes.Create(f.ctx, c.Inbox{ID: otherInbox, TenantID: f.tenant, Name: "Suspended assignees", Status: "active", AutoAssignEnabled: true, CreatedAt: now, UpdatedAt: now})
		must(t, e)
		_, e = repos.Memberships.Put(f.ctx, c.InboxUser{ID: uuid.New(), TenantID: f.tenant, InboxID: otherInbox, UserID: inactiveUser.String(), Capacity: 2, AutoAssignEnabled: true, CreatedAt: now})
		must(t, e)
		_, e = f.db.ExecContext(f.ctx, "UPDATE `"+f.database+"`.identity_users SET active=FALSE WHERE id=?", id(inactiveUser))
		must(t, e)
		blocked, e := repos.Cases.Create(f.ctx, c.Case{ID: uuid.New(), TenantID: f.tenant, InboxID: otherInbox, Name: "Older case with no eligible assignee", Status: c.StatusPending, Outcome: c.OutcomeUnset, Type: c.TypeDecision, CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now})
		must(t, e)
		n, e := m.AssignWaiting(f.ctx, 100)
		must(t, e)
		if n != 1 {
			t.Fatalf("assigned=%d", n)
		}
		n, e = m.AssignWaiting(f.ctx, 100)
		must(t, e)
		if n != 0 {
			t.Fatal("capacity exceeded")
		}
		unchanged, e := repos.Cases.Get(f.ctx, f.tenant, blocked.ID)
		must(t, e)
		if unchanged.AssignedTo != nil {
			t.Fatal("suspended investigator received an assignment")
		}
		a, e := repos.Cases.Get(f.ctx, f.tenant, one.ID)
		must(t, e)
		b, e := repos.Cases.Get(f.ctx, f.tenant, two.ID)
		must(t, e)
		assigned := 0
		for _, v := range []c.Case{a, b} {
			if v.AssignedTo != nil {
				assigned++
			}
		}
		if assigned != 1 {
			t.Fatal("assignment count", assigned)
		}
	})
	t.Run("expired delivery claim cannot finish", func(t *testing.T) {
		d, e := m.ClaimDelivery(f.ctx)
		must(t, e)
		if d == nil {
			t.Fatal("expected delivery")
		}
		_, e = f.db.Exec("UPDATE `"+f.database+"`.case_manager_outbox_events SET lease_until=DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 1 SECOND) WHERE id=?", id(d.ID))
		must(t, e)
		if !errors.Is(m.FinishDelivery(f.ctx, *d, ""), c.ErrConflict) {
			t.Fatal("expired lease finished")
		}
		reclaimed, e := m.ClaimDelivery(f.ctx)
		must(t, e)
		if reclaimed == nil || reclaimed.ID != d.ID || reclaimed.Token == d.Token {
			t.Fatal("claim not recovered")
		}
		must(t, m.FinishDelivery(f.ctx, *reclaimed, ""))
	})
	t.Run("inbox delivery and retry terminal failure", func(t *testing.T) {
		must(t, f.uow().WithinTransaction(f.ctx, func(r ports.Repositories) error {
			actor := f.user.String()
			return r.Memberships.RecordChange(f.ctx, f.tenant, f.inbox, &actor, "inbox_user_updated", f.user.String(), true, time.Now().UTC(), 1)
		}))
		for {
			d, e := m.ClaimDelivery(f.ctx)
			must(t, e)
			if d == nil {
				t.Fatal("inbox delivery not found")
			}
			if d.AggregateType == "case" {
				must(t, m.FinishDelivery(f.ctx, *d, ""))
				continue
			}
			for {
				must(t, m.FinishDelivery(f.ctx, *d, "publisher unavailable"))
				if d.Attempt >= 12 {
					break
				}
				_, e = f.db.Exec("UPDATE `"+f.database+"`.case_manager_inbox_outbox SET next_attempt_at=DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 1 SECOND) WHERE id=?", id(d.ID))
				must(t, e)
				d, e = m.ClaimDelivery(f.ctx)
				must(t, e)
				if d == nil {
					t.Fatal("retry missing")
				}
			}
			var status string
			must(t, f.db.QueryRow("SELECT status FROM `"+f.database+"`.case_manager_inbox_outbox WHERE id=?", id(d.ID)).Scan(&status))
			if status != "failed" {
				t.Fatal(status)
			}
			must(t, m.Replay(f.ctx, f.tenant, d.ID, true))
			break
		}
	})
}
