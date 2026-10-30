package mysql

import (
	"math"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/google/uuid"
)

func TestEffectRetryIdentityAndEligibility(t *testing.T) {
	for _, kind := range []ports.JobKind{ports.JobWorkflow, ports.JobScreening, ports.JobScoring, ports.JobOutbox} {
		v := mysqljobs.Identity{Version: 1, Kind: kind, TenantID: uuid.New(), ResourceID: uuid.New(), DomainAttempt: 1}
		next, err := nextEffectIdentity(v, EffectDomainRetry, 3)
		if err != nil || next.DomainAttempt != 2 || next.RedriveGeneration != 0 || next.ResourceID != v.ResourceID || next.TenantID != v.TenantID || next.Kind != kind {
			t.Fatal("domain retry identity", next, err)
		}
		next, err = nextEffectIdentity(next, EffectRedrive, 3)
		if err != nil || next.DomainAttempt != 2 || next.RedriveGeneration != 1 {
			t.Fatal("redrive replenished or reset domain attempt", next, err)
		}
		next, err = nextEffectIdentity(next, EffectDomainRetry, 3)
		if err != nil || next.DomainAttempt != 3 || next.RedriveGeneration != 1 {
			t.Fatal("domain retry regressed generation", next, err)
		}
		if _, err := nextEffectIdentity(next, EffectDomainRetry, 3); err == nil {
			t.Fatal("ignored domain budget")
		}
		next.RedriveGeneration = math.MaxInt32
		if _, err := nextEffectIdentity(next, EffectRedrive, 3); err == nil {
			t.Fatal("generation overflow")
		}
		for _, status := range []string{"pending", "pending_dispatch", "queued", "completed", "dispatched", "cancelled", "unknown"} {
			if effectRetryEligible(kind, status, EffectDomainRetry) || effectRetryEligible(kind, status, EffectRedrive) {
				t.Fatal("accepted ineligible owner", kind, status)
			}
		}
		failed := "failed"
		if kind == ports.JobWorkflow {
			failed = "dispatch_failed"
		}
		if !effectRetryEligible(kind, failed, EffectDomainRetry) || !effectRetryEligible(kind, failed, EffectRedrive) || effectRetryEligible(kind, failed, "unknown") {
			t.Fatal("owner/action contract", kind)
		}
		provider := kind == ports.JobScreening || kind == ports.JobScoring
		if effectRetryEligible(kind, "sent", EffectDomainRetry) || effectRetryEligible(kind, "sent", EffectRedrive) != provider {
			t.Fatal("uncertain provider send may only be explicitly redriven", kind)
		}
	}
	for _, kind := range []ports.JobKind{ports.JobAsync, ports.JobCallback, ports.JobScheduled, "unknown"} {
		v := mysqljobs.Identity{Version: 1, Kind: kind, TenantID: uuid.New(), ResourceID: uuid.New(), DomainAttempt: 1}
		if _, err := nextEffectIdentity(v, EffectDomainRetry, 3); err == nil {
			t.Fatal("accepted another owner catalog", kind)
		}
	}
	var store *EffectRetryStore
	if _, err := store.Retry(t.Context(), mysqljobs.Identity{}); err == nil {
		t.Fatal("nil store reported successful retry")
	}
}
