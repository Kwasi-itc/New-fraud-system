package mysql

import (
	"context"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

func TestMySQLEffectProviderSentRequiresExplicitRedrive(t *testing.T) {
	f := effectsFixture(t, true)
	ctx := context.Background()
	ids := createRetryEffects(t, f)
	schema, _ := mysqlstore.Identifier(f.database)
	for _, kind := range []ports.JobKind{ports.JobScreening, ports.JobScoring} {
		t.Run(string(kind), func(t *testing.T) {
			transport, retry := retryStores(t, f, kind)
			lease := claimRetryEffect(t, transport)
			if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+"."+effectOwnerTable(kind)+" SET provider_reference='old-ref',response_json=JSON_OBJECT('old',TRUE),last_error='old-error',sent_at=UTC_TIMESTAMP(6),failed_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(ids[kind])); err != nil {
				t.Fatal(err)
			}
			finishRetryEffect(t, f, transport, lease, "sent")
			if _, err := retry.Retry(ctx, lease.Identity); !errors.Is(err, ErrEffectRetryState) {
				t.Fatal("automatically retried uncertain send", err)
			}
			next, err := retry.Redrive(ctx, lease.Identity)
			if err != nil || next.DomainAttempt != 1 || next.RedriveGeneration != 1 {
				t.Fatal(next, err)
			}
			var status, ref, last, response string
			var cleared bool
			if err := f.db.QueryRowContext(ctx, "SELECT status,provider_reference,last_error,CAST(response_json AS CHAR),sent_at IS NULL AND completed_at IS NULL AND failed_at IS NULL FROM "+schema+"."+effectOwnerTable(kind)+" WHERE tenant_id=? AND id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(ids[kind])).Scan(&status, &ref, &last, &response, &cleared); err != nil {
				t.Fatal(err)
			}
			if status != "pending" || ref != "" || last != "" || response != "{}" || !cleared {
				t.Fatal("incomplete source-compatible provider reset", status, ref, last, response, cleared)
			}
		})
	}
}
