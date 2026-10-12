package postgres_test

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/google/uuid"
	"testing"
)

func TestDecisionDispatchPublicationAndRollback(t *testing.T) {
	db := deliveryDatabase(t)
	ctx := context.Background()
	tenant := uuid.NewString()
	for _, tc := range []struct {
		name            string
		decision        bool
		partial         bool
		matches, events int
		status          string
	}{
		{"matched", true, false, 2, 1, "awaiting_review"}, {"partial-empty", true, true, 0, 1, "awaiting_review"}, {"complete-empty", true, false, 0, 0, "no_hit"}, {"freeform-unchanged", false, true, 0, 0, "no_hit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.NewString()
			var decision *string
			if tc.decision {
				v := uuid.NewString()
				decision = &v
			}
			if _, err := db.Exec(ctx, `INSERT INTO screening.screenings(id,tenant_id,decision_id,provider,object_type,object_id,status,created_at,updated_at) VALUES($1,$2,$3,'test','customer','customer-1','pending',now(),now())`, id, tenant, decision); err != nil {
				t.Fatal(err)
			}
			svc := dispatch(db, resultProvider(tc.partial, tc.matches))
			if tc.events > 0 {
				if _, err := db.Exec(ctx, `CREATE FUNCTION screening.reject_decision_arrival() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected publication failure'; END $$; CREATE TRIGGER reject_decision_arrival BEFORE INSERT ON screening.case_event_outbox FOR EACH ROW EXECUTE FUNCTION screening.reject_decision_arrival()`); err != nil {
					t.Fatal(err)
				}
				if err := svc.RunScreening(ctx, tenant, id); err == nil {
					t.Fatal("ignored publication failure")
				}
				var n int
				var status string
				if err := db.QueryRow(ctx, `SELECT status,(SELECT count(*) FROM screening.screening_matches WHERE screening_id=$1) FROM screening.screenings WHERE id=$1`, id).Scan(&status, &n); err != nil || status != string(screening.StatusPending) || n != 0 {
					t.Fatal("partial publication", status, n, err)
				}
				if _, err := db.Exec(ctx, `DROP TRIGGER reject_decision_arrival ON screening.case_event_outbox; DROP FUNCTION screening.reject_decision_arrival()`); err != nil {
					t.Fatal(err)
				}
			}
			for i := 0; i < 2; i++ {
				if err := svc.RunScreening(ctx, tenant, id); err != nil {
					t.Fatal(err)
				}
			}
			var status string
			var events, matches int
			if err := db.QueryRow(ctx, `SELECT status,(SELECT count(*) FROM screening.case_event_outbox WHERE payload->>'screening_id'=$1::uuid::text),(SELECT count(*) FROM screening.screening_matches WHERE screening_id=$1) FROM screening.screenings WHERE id=$1`, id).Scan(&status, &events, &matches); err != nil || status != tc.status || events != tc.events || matches != tc.matches {
				t.Fatal("wrong publication", status, events, matches, err)
			}
		})
	}
}
