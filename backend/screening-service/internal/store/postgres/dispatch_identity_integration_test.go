package postgres_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	"github.com/google/uuid"
)

func TestDispatchUsesApplicationCounterpartyIdentity(t *testing.T) {
	db := deliveryDatabase(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `CREATE TABLE screening.test_enqueued(tenant_id uuid,screening_id uuid)`); err != nil {
		t.Fatal(err)
	}
	svc := intakeService(db, intakeEnqueuer{})
	trusted, providerValue, blank := "customer:synthetic-1", "provider-supplied-other-customer", "   "
	for _, tc := range []struct {
		name     string
		parent   *string
		provider *string
		want     *string
	}{
		{"provider cannot override application identity", &trusted, &providerValue, &trusted},
		{"missing provider echo preserves application identity", &trusted, nil, &trusted},
		{"provider cannot introduce identity for manual search", nil, &providerValue, nil},
		{"blank application identity is not an exclusion key", &blank, &providerValue, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tenant, actor := uuid.NewString(), uuid.NewString()
			run, err := svc.CreateManualSearch(ctx, tenant, actor, service.ManualSearchInput{OperationID: uuid.NewString(), Name: "Synthetic", EntityType: "Person", Threshold: 70, Limit: 10})
			if err != nil {
				t.Fatal(err)
			}
			// Simulate a persisted application-supplied identity as scenario
			// intake stores it. Leave the serialized request untrusted for this
			// assertion: ownership comes from the parent record.
			if _, err := db.Exec(ctx, `UPDATE screening.screenings SET unique_counterparty_identifier=$2 WHERE id=$1`, run.ID, tc.parent); err != nil {
				t.Fatal(err)
			}
			provider := intakeProvider{search: func(context.Context, screening.SearchRequest) (screening.ProviderResult, error) {
				return screening.ProviderResult{RawResponse: json.RawMessage(`{}`), Matches: []screening.ProviderMatch{{EntityID: "same-provider-entity", Name: "Synthetic match", Payload: json.RawMessage(`{}`), UniqueCounterpartyIdentifier: tc.provider}}}, nil
			}}
			if err := dispatch(db, provider).RunScreening(ctx, tenant, run.ID); err != nil {
				t.Fatal(err)
			}
			details, err := svc.GetManualSearch(ctx, tenant, actor, run.ID)
			if err != nil || len(details.Matches) != 1 {
				t.Fatal("missing result", err)
			}
			got := details.Matches[0].UniqueCounterpartyIdentifier
			if (got == nil) != (tc.want == nil) || got != nil && *got != *tc.want {
				t.Fatalf("candidate identity does not follow parent: got %v want %v", got, tc.want)
			}
		})
	}
}

func TestManualIncompleteEmptyResultRemainsUnresolved(t *testing.T) {
	db := deliveryDatabase(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `CREATE TABLE screening.test_enqueued(tenant_id uuid,screening_id uuid)`); err != nil {
		t.Fatal(err)
	}
	tenant, actor := uuid.NewString(), uuid.NewString()
	svc := intakeService(db, intakeEnqueuer{})
	run, err := svc.CreateManualSearch(ctx, tenant, actor, service.ManualSearchInput{OperationID: uuid.NewString(), Name: "Synthetic", EntityType: "Person", Threshold: 70, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if err := dispatch(db, resultProvider(true, 0)).RunScreening(ctx, tenant, run.ID); err != nil {
		t.Fatal(err)
	}
	details, err := svc.GetManualSearch(ctx, tenant, actor, run.ID)
	if err != nil || details.Screening.Status != screening.StatusAwaitingReview || !details.Screening.Partial || len(details.Matches) != 0 {
		t.Fatal("incomplete empty search was treated as a negative result", details.Screening.Status, err)
	}
	for _, status := range []string{"awaiting_review", "no_hit"} {
		history, err := svc.ListManualSearches(ctx, tenant, actor, screening.ManualHistoryFilter{Status: status, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if status == "awaiting_review" && (len(history.Items) != 1 || !history.Items[0].Partial) {
			t.Fatal("unresolved partial missing from history")
		}
		if status == "no_hit" && len(history.Items) != 0 {
			t.Fatal("partial included in negative search history")
		}
	}
	var events int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM screening.case_event_outbox WHERE tenant_id=$1`, tenant).Scan(&events); err != nil || events != 0 {
		t.Fatal("manual partial search unexpectedly created an automatic case event", events, err)
	}
}
