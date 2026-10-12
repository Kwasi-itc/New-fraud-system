package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
	"github.com/google/uuid"
)

func TestPostgresLifecycleParity(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, inbox, _ := fixture(t, db)
	ctx := userContext(tenant, "analyst-1")
	for _, bulk := range []bool{false, true} {
		for _, assigned := range []bool{false, true} {
			for _, kind := range []casepkg.Type{casepkg.TypeDecision, casepkg.TypeContinuousScreening} {
				t.Run(fmt.Sprintf("bulk=%v/assigned=%v/%s", bulk, assigned, kind), func(t *testing.T) {
					c, err := s.CreateCase(serviceContext(tenant), service.CreateCaseInput{TenantID: tenant, InboxID: inbox.ID, Name: "Lifecycle", Type: kind}, nil)
					if err != nil {
						t.Fatal(err)
					}
					if assigned {
						user := "analyst-1"
						if err := s.Assign(ctx, tenant, c.ID, &user, nil); err != nil {
							t.Fatal(err)
						}
					}
					// An automated pending case can already be snoozed/boosted. Closing
					// must not produce an intermediate investigation or claim it.
					if _, err := db.Exec(ctx, `UPDATE case_manager.cases SET snoozed_until=now()+interval '1 hour',boost_reason='new_decision' WHERE id=$1`, c.ID); err != nil {
						t.Fatal(err)
					}
					input := service.BulkInput{OperationID: uuid.New(), CaseIDs: []uuid.UUID{c.ID}, Action: "close", Outcome: casepkg.OutcomeConfirmedRisk, Comment: "  Verified findings  "}
					if bulk {
						results, err := s.Bulk(ctx, tenant, input)
						if err != nil || !results[0].Success {
							t.Fatalf("close: %+v %v", results, err)
						}
					} else if _, err := s.CloseCase(ctx, tenant, c.ID, input.Outcome, input.Comment); err != nil {
						t.Fatal(err)
					}
					closed, err := s.GetCase(ctx, tenant, c.ID)
					if err != nil || closed.Status != casepkg.StatusClosed || closed.Outcome != input.Outcome || closed.SnoozedUntil != nil || closed.BoostReason != nil || (closed.AssignedTo != nil) != assigned {
						t.Fatalf("close state: %+v %v", closed, err)
					}
					findings := 0
					for _, event := range closed.Events {
						if event.EventType == "status_updated" && event.NewValue == "investigating" {
							t.Fatal("close started investigation")
						}
						if event.AdditionalNote == "Verified findings" {
							findings++
							if event.UserID == nil || *event.UserID != "analyst-1" {
								t.Fatal("untrusted findings actor")
							}
						}
					}
					if findings != 1 || len(closed.Contributors) != 1 {
						t.Fatalf("findings=%d contributors=%+v", findings, closed.Contributors)
					}
					before := count(t, db, "case_events")
					if bulk {
						r, err := s.Bulk(ctx, tenant, input)
						if err != nil || !r[0].Replayed {
							t.Fatalf("replay: %+v %v", r, err)
						}
						input.OperationID = uuid.New()
						r, err = s.Bulk(ctx, tenant, input)
						if err != nil || r[0].Error != "conflict" {
							t.Fatalf("repeat close: %+v %v", r, err)
						}
					} else if _, err := s.CloseCase(ctx, tenant, c.ID, input.Outcome, "duplicate"); !errors.Is(err, casepkg.ErrConflict) {
						t.Fatal("repeat close", err)
					}
					if count(t, db, "case_events") != before {
						t.Fatal("repeat close created history")
					}
					// Include legacy stale attention state to verify both reopen paths.
					if _, err := db.Exec(ctx, `UPDATE case_manager.cases SET snoozed_until=now()+interval '1 hour',boost_reason='escalated' WHERE id=$1`, c.ID); err != nil {
						t.Fatal(err)
					}
					if bulk {
						r, err := s.Bulk(ctx, tenant, service.BulkInput{OperationID: uuid.New(), CaseIDs: []uuid.UUID{c.ID}, Action: "reopen"})
						if err != nil || !r[0].Success {
							t.Fatalf("reopen: %+v %v", r, err)
						}
					} else {
						status := casepkg.StatusInvestigating
						if _, err := s.UpdateCase(ctx, service.UpdateCaseInput{TenantID: tenant, CaseID: c.ID, Status: &status}, nil); err != nil {
							t.Fatal(err)
						}
					}
					opened, err := s.GetCase(ctx, tenant, c.ID)
					if err != nil || opened.Status != casepkg.StatusInvestigating || opened.Outcome != casepkg.OutcomeUnset || opened.SnoozedUntil != nil || opened.BoostReason != nil || (opened.AssignedTo != nil) != assigned {
						t.Fatalf("reopen state: %+v %v", opened, err)
					}
					if !bulk {
						status := casepkg.StatusInvestigating
						repeated, err := s.UpdateCase(ctx, service.UpdateCaseInput{TenantID: tenant, CaseID: c.ID, Status: &status}, nil)
						if err != nil || (repeated.AssignedTo != nil) != assigned || len(repeated.Events) != len(opened.Events) {
							t.Fatalf("repeated reopen changed assignment/history: %+v %v", repeated, err)
						}
					}
				})
			}
		}
	}
}

func TestPostgresLifecycleReopenGuardsAndRollback(t *testing.T) {
	for _, bulk := range []bool{false, true} {
		t.Run(fmt.Sprintf("bulk=%v", bulk), func(t *testing.T) {
			db := testDatabase(t, true)
			s, tenant, inbox, c := fixture(t, db)
			admin := adminContext(tenant)
			user := "analyst-1"
			if err := s.Assign(admin, tenant, c.ID, &user, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := s.CloseCase(admin, tenant, c.ID, casepkg.OutcomeConfirmedRisk, "Original findings"); err != nil {
				t.Fatal(err)
			}
			attempt := func(ctx context.Context) error {
				if bulk {
					r, err := s.Bulk(ctx, tenant, service.BulkInput{OperationID: uuid.New(), CaseIDs: []uuid.UUID{c.ID}, Action: "reopen"})
					if err != nil {
						return err
					}
					if !r[0].Success {
						return fmt.Errorf("%s", r[0].Error)
					}
					return nil
				}
				status := casepkg.StatusInvestigating
				_, err := s.UpdateCase(ctx, service.UpdateCaseInput{TenantID: tenant, CaseID: c.ID, Status: &status}, nil)
				return err
			}
			unchanged := func(ctx context.Context) {
				t.Helper()
				before, outbox := count(t, db, "case_events"), count(t, db, "outbox_events")
				if err := attempt(ctx); err == nil {
					t.Fatal("invalid reopen succeeded")
				}
				item, err := s.GetCase(admin, tenant, c.ID)
				if err != nil || item.Status != casepkg.StatusClosed || item.Outcome != casepkg.OutcomeConfirmedRisk || count(t, db, "case_events") != before || count(t, db, "outbox_events") != outbox {
					t.Fatalf("failed reopen mutated state: %+v %v", item, err)
				}
			}
			unchanged(userContext(tenant, "outsider"))
			unchanged(userContext(uuid.New(), user))
			unchanged(serviceContext(tenant))
			if _, err := s.PutInboxUser(admin, tenant, inbox.ID, user, true, 0); err != nil {
				t.Fatal(err)
			}
			unchanged(admin) // Managers cannot bypass capacity.
			if err := s.RemoveInboxUser(admin, tenant, inbox.ID, user); err != nil {
				t.Fatal(err)
			}
			unchanged(admin)
			if err := s.Assign(admin, tenant, c.ID, nil, nil); err != nil {
				t.Fatal(err)
			}
			archived := "archived"
			if _, err := s.UpdateInbox(admin, service.UpdateInboxInput{TenantID: tenant, InboxID: inbox.ID, Status: &archived}); err != nil {
				t.Fatal(err)
			}
			unchanged(admin)
			active := "active"
			if _, err := s.UpdateInbox(admin, service.UpdateInboxInput{TenantID: tenant, InboxID: inbox.ID, Status: &active}); err != nil {
				t.Fatal(err)
			}
			failInserts(t, db, "outbox_events")
			unchanged(admin)
			if _, err := db.Exec(admin, `DROP TRIGGER fail_test_write ON case_manager.outbox_events`); err != nil {
				t.Fatal(err)
			}
			if err := attempt(admin); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPostgresLifecyclePreservesOwnerDispositions(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, _, c := fixture(t, db)
	decision := seedDecision(t, db, tenant)
	screening, match, _ := seedScreening(t, db, tenant, decision)
	if _, err := s.AddDecision(serviceContext(tenant), service.AddDecisionInput{TenantID: tenant, CaseID: c.ID, DecisionID: decision}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.HandleScreeningReviewed(serviceContext(tenant), tenant, screening, &decision, match, "pending", nil); err != nil {
		t.Fatal(err)
	}
	ctx := userContext(tenant, "analyst-1")
	var before, after, matchStatus string
	if err := db.QueryRow(ctx, `SELECT to_jsonb(d)::text FROM core.decisions d WHERE id=$1`, decision).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CloseCase(ctx, tenant, c.ID, casepkg.OutcomeFalsePositive, "Case resolution only"); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT to_jsonb(d)::text FROM core.decisions d WHERE id=$1`, decision).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT status FROM screening.screening_matches WHERE id=$1`, match).Scan(&matchStatus); err != nil {
		t.Fatal(err)
	}
	if before != after || matchStatus != "pending" {
		t.Fatal("close altered owner disposition")
	}
}

func TestPostgresLifecycleCloseAtCapacityAndConcurrentRetry(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, inbox, c := fixture(t, db)
	ctx := userContext(tenant, "analyst-1")
	if _, err := s.PutInboxUser(adminContext(tenant), tenant, inbox.ID, "analyst-1", true, 0); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			_, err := s.CloseCase(ctx, tenant, c.ID, casepkg.OutcomeFalsePositive, "Capacity does not block resolution")
			results <- err
		}()
	}
	close(start)
	success, conflict := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			success++
		} else if errors.Is(err, casepkg.ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	item, err := s.GetCase(ctx, tenant, c.ID)
	if err != nil || item.AssignedTo != nil {
		t.Fatalf("closing claimed case: %+v %v", item, err)
	}
	comments := 0
	for _, event := range item.Events {
		if event.EventType == "comment_added" {
			comments++
		}
	}
	if comments != 1 {
		t.Fatalf("duplicate findings: %d", comments)
	}
}

func TestPostgresLifecycleFailuresAreAtomic(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, _, c := fixture(t, db)
	ctx := userContext(tenant, "analyst-1")
	before, outbox := count(t, db, "case_events"), count(t, db, "outbox_events")
	for _, comment := range []string{" \t", strings.Repeat("é", 5001)} {
		if _, err := s.CloseCase(ctx, tenant, c.ID, casepkg.OutcomeFalsePositive, comment); !errors.Is(err, casepkg.ErrValidation) {
			t.Fatal(err)
		}
	}
	for _, forbidden := range []context.Context{userContext(tenant, "outsider"), userContext(uuid.New(), "analyst-1"), serviceContext(tenant)} {
		if _, err := s.CloseCase(forbidden, tenant, c.ID, casepkg.OutcomeFalsePositive, "forbidden"); !errors.Is(err, casepkg.ErrForbidden) {
			t.Fatal(err)
		}
	}
	failInserts(t, db, "outbox_events")
	if _, err := s.CloseCase(ctx, tenant, c.ID, casepkg.OutcomeFalsePositive, "rollback"); err == nil {
		t.Fatal("ignored outbox failure")
	}
	r, err := s.Bulk(ctx, tenant, service.BulkInput{OperationID: uuid.New(), CaseIDs: []uuid.UUID{c.ID}, Action: "close", Outcome: casepkg.OutcomeFalsePositive, Comment: "rollback"})
	if err != nil || r[0].Success {
		t.Fatalf("bulk rollback %+v %v", r, err)
	}
	item, err := s.GetCase(ctx, tenant, c.ID)
	if err != nil || item.Status != casepkg.StatusPending || item.Outcome != casepkg.OutcomeUnset || item.AssignedTo != nil || len(item.Contributors) != 0 || count(t, db, "case_events") != before || count(t, db, "outbox_events") != outbox {
		t.Fatalf("failed close changed state: %+v %v", item, err)
	}
	// Bound the query as a sanity check that no bulk success receipt escaped.
	check, cancel := context.WithTimeout(ctx, time.Second*5)
	defer cancel()
	var receipts int
	if err := db.QueryRow(check, `SELECT count(*) FROM case_manager.intake_receipts WHERE tenant_id=$1 AND source='bulk'`, tenant).Scan(&receipts); err != nil || receipts != 0 {
		t.Fatalf("receipt escaped: %d %v", receipts, err)
	}
}
