package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/store/postgres"
)

func TestPhase4AssignmentAcrossFullInboxBacklog(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, full, c := fixture(t, db)
	admin := adminContext(tenant)
	enabled := true
	if _, err := s.PutInboxUser(admin, tenant, full.ID, "analyst-1", true, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateInbox(admin, service.UpdateInboxInput{TenantID: tenant, InboxID: full.ID, AutoAssignEnabled: &enabled}); err != nil {
		t.Fatal(err)
	}
	user := "analyst-1"
	if err := s.Assign(admin, tenant, c.ID, &user, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(admin, `INSERT INTO case_manager.cases(id,tenant_id,inbox_id,name,status,outcome,type,created_at,updated_at) SELECT gen_random_uuid(),$1,$2,'Backlog','pending','unset','decision','2020-01-01',now() FROM generate_series(1,10000)`, tenant, full.ID); err != nil {
		t.Fatal(err)
	}
	available, err := s.CreateInbox(admin, service.CreateInboxInput{TenantID: tenant, Name: "Available"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutInboxUser(admin, tenant, available.ID, user, true, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateInbox(admin, service.UpdateInboxInput{TenantID: tenant, InboxID: available.ID, AutoAssignEnabled: &enabled}); err != nil {
		t.Fatal(err)
	}
	next, err := s.CreateCase(serviceContext(tenant), service.CreateCaseInput{TenantID: tenant, InboxID: available.ID, Name: "Eligible"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(admin, `ANALYZE case_manager.cases; ANALYZE case_manager.inbox_users; ANALYZE case_manager.inboxes`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if n, err := (store.Maintenance{DB: db}).AssignWaiting(ctx, 10); err != nil || n != 1 {
		t.Fatal("full inbox starved available inbox", n, err)
	}
	item, err := s.WorkspaceOverview(admin, tenant, next.ID)
	if err != nil || item.AssignedTo == nil || *item.AssignedTo != user {
		t.Fatal("eligible case not assigned", err)
	}
	var plan string
	if err := db.QueryRow(admin, `EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) SELECT count(*) FROM case_manager.cases WHERE tenant_id=$1 AND inbox_id=$2 AND assigned_to=$3 AND status<>'closed'`, tenant, full.ID, user).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan, "cases_assignment_workload_idx") {
		t.Fatal("workload did not use assignment index", plan)
	}
}
