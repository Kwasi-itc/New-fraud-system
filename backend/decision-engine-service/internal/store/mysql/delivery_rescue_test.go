package mysql

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
	"github.com/google/uuid"
)

func TestDeliveryFailureUsesErrorHistoryAndNeverReportsSuccess(t *testing.T) {
	for _, test := range []struct {
		attempt, max int
		errors       int64
		state        workers.DeliveryState
		seconds      float64
	}{{1, 3, 0, workers.Available, 1}, {3, 5, 1, workers.Available, 16}, {3, 3, 2, workers.Discarded, 0}, {1, 1, 0, workers.Discarded, 0}} {
		state, delay, err := deliveryFailurePlan(test.attempt, test.max, test.errors)
		if err != nil || state != test.state || delay.Seconds() < test.seconds*.9 || delay.Seconds() > test.seconds*1.1 {
			t.Fatal(test, state, delay, err)
		}
	}
	for _, test := range [][3]int{{0, 3, 0}, {1, 0, 0}, {4, 3, 1}, {1, 3, -1}, {1, 3, 1}, {2, 3, 3}} {
		if _, _, err := deliveryFailurePlan(test[0], test[1], int64(test[2])); err == nil {
			t.Fatal("invented/overflowing error history accepted", test)
		}
	}
}

func TestExpiredDeliveryRejectsForgedScopeAndUninitializedAdapters(t *testing.T) {
	tenant, resource := uuid.New(), uuid.New()
	allow := func(context.Context, *sql.Tx, uuid.UUID, ports.JobKind, EffectWorkerAction) error { return nil }
	for _, kind := range []ports.JobKind{ports.JobWorkflow, ports.JobScreening, ports.JobScoring, ports.JobOutbox, ports.JobScheduled, ports.JobAsync, ports.JobCallback} {
		s, err := newScopedDeliveryStore(&sql.DB{}, "fraud", tenant, kind, "queue", time.Minute, allow, executionOwner(kind) != "")
		if err != nil {
			t.Fatal(err)
		}
		v := ExpiredDelivery{ID: uuid.New(), Token: uuid.New(), Identity: mysqljobs.Identity{Version: 1, Kind: kind, TenantID: tenant, ResourceID: resource, DomainAttempt: 1}, TransportAttempt: 1, LeaseUntil: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
		if !s.validExpired(v) {
			t.Fatal("valid candidate rejected", kind)
		}
		for _, change := range []func(*ExpiredDelivery){func(v *ExpiredDelivery) { v.Token = uuid.Nil }, func(v *ExpiredDelivery) { v.Identity.TenantID = uuid.New() }, func(v *ExpiredDelivery) { v.Identity.Kind = "unknown" }, func(v *ExpiredDelivery) { v.TransportAttempt = 0 }, func(v *ExpiredDelivery) { v.LeaseUntil = v.LeaseUntil.Add(time.Nanosecond) }, func(v *ExpiredDelivery) { v.LeaseUntil = time.Time{} }} {
			bad := v
			change(&bad)
			if s.validExpired(bad) {
				t.Fatal("forged candidate accepted", bad)
			}
		}
		for _, limit := range []int{-1, 0, 101} {
			if _, err := s.Expired(context.Background(), nil, limit); err == nil {
				t.Fatal("unbounded expired page")
			}
		}
		if _, err := s.Expired(context.Background(), &ExpiredDeliveryCursor{}, 1); err == nil {
			t.Fatal("invalid keyset cursor accepted")
		}
		if !s.executionKinds {
			if _, err := s.Rescue(context.Background(), v, nil); err == nil {
				t.Fatal("missing exhaustion provider accepted")
			}
		}
	}
	if _, err := (*ExecutionDeliveryStore)(nil).Rescue(context.Background(), nil, ExpiredDelivery{}); err == nil {
		t.Fatal("nil rescue succeeded")
	}
}

func TestExecutionRescueOwnerEligibilityRequiresExactDomainGenerationAndOwnership(t *testing.T) {
	id, tenant := uuid.New(), uuid.New()
	identity := mysqljobs.Identity{Version: 1, Kind: ports.JobScheduled, TenantID: tenant, ResourceID: id, DomainAttempt: 1}
	scheduled := ScheduledExecutionRecord{ScheduledExecution: execution.ScheduledExecution{ID: id.String(), TenantID: tenant.String(), Status: execution.StatusPending, MaxAttempts: 3}}
	if err := validateScheduledDelivery(scheduled, identity); err != nil {
		t.Fatal(err)
	}
	scheduled.Status, scheduled.AttemptCount = execution.StatusRunning, 1
	if err := validateScheduledDelivery(scheduled, identity); err != nil {
		t.Fatal("same domain resume rejected", err)
	}
	for _, change := range []func(*mysqljobs.Identity){func(v *mysqljobs.Identity) { v.DomainAttempt = 2 }, func(v *mysqljobs.Identity) { v.RedriveGeneration = 1 }, func(v *mysqljobs.Identity) { v.TenantID = uuid.New() }, func(v *mysqljobs.Identity) { v.ResourceID = uuid.New() }, func(v *mysqljobs.Identity) { v.Kind = ports.JobAsync }} {
		bad := identity
		change(&bad)
		if validateScheduledDelivery(scheduled, bad) == nil {
			t.Fatal("ineligible scheduled recovery")
		}
	}
	identity.Kind = ports.JobCallback
	async := AsyncExecutionRecord{AsyncDecisionExecution: execution.AsyncDecisionExecution{ID: id.String(), TenantID: tenant.String(), Status: execution.StatusCompleted, CallbackURL: "https://callback.invalid", CallbackStatus: "running", CallbackAttemptCount: 1}, CallbackMaxAttempts: 3}
	if err := validateAsyncDelivery(async, identity); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*AsyncExecutionRecord){func(v *AsyncExecutionRecord) { v.Status = execution.StatusRunning }, func(v *AsyncExecutionRecord) { v.CallbackStatus = "sent" }, func(v *AsyncExecutionRecord) { v.CallbackURL = "" }, func(v *AsyncExecutionRecord) { v.Generation = 1 }, func(v *AsyncExecutionRecord) { v.CallbackAttemptCount = 2 }} {
		bad := async
		change(&bad)
		if validateAsyncDelivery(bad, identity) == nil {
			t.Fatal("ineligible callback recovery")
		}
	}
}

func TestExpiredDeliveryQueryIsBoundedParameterizedAndUsesObservedFence(t *testing.T) {
	s := &EffectDeliveryStore{schema: "`fraud`", tenant: uuid.New(), kind: ports.JobOutbox, queue: "custom_queue"}
	after := &ExpiredDeliveryCursor{LeaseUntil: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ID: uuid.New()}
	for _, cursor := range []*ExpiredDeliveryCursor{nil, after} {
		query, args := s.expiredSelect(cursor, 100)
		if strings.Count(query, "?") != len(args) || !strings.HasSuffix(query, "ORDER BY d.lease_until,d.id LIMIT ?") || strings.Contains(query, s.tenant.String()) || strings.Contains(query, s.queue) || strings.Contains(query, "FOR UPDATE") || strings.Contains(query, "request_body") {
			t.Fatal(query, args)
		}
	}
	for _, part := range []string{"lease_token=?", "attempt=?", "lease_until=?", "lease_until<=UTC_TIMESTAMP(6)"} {
		if !strings.Contains(expiredDeliveryPredicate, part) {
			t.Fatal("missing observed expiry fence", part)
		}
	}
}
