package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"os"
	"testing"
	"time"
)

func TestMySQLProviderScoringResultIsIdempotentAndGenerationFenced(t *testing.T) {
	for _, name := range []string{"apply", "stale", "failed", "pending", "reference", "different_completed"} {
		stale := name == "stale"
		t.Run(name, func(t *testing.T) {
			f, _, owner := dispatchFixture(t)
			ctx := context.Background()
			delivery, retry := retryStores(t, f, ports.JobScoring)
			dispatch, err := NewEffectDispatchStore(owner, delivery)
			if err != nil {
				t.Fatal(err)
			}
			lease, err := dispatch.Claim(ctx)
			if err != nil || lease == nil {
				t.Fatal(lease, err)
			}
			intent, err := dispatch.Prepare(ctx, *lease)
			if err != nil {
				t.Fatal(err)
			}
			reference := "accepted-provider-receipt"
			receipt := ports.DispatchReceipt{Key: intent.Request.Key, PayloadSHA256: intent.Request.PayloadSHA256, Outcome: ports.DispatchAccepted, Reference: reference}
			if err := dispatch.Complete(ctx, *lease, intent.Request, receipt, time.Second); err != nil {
				t.Fatal(err)
			}
			if stale {
				if _, err := retry.Redrive(ctx, lease.Identity); err != nil {
					t.Fatal(err)
				}
			}
			if name != "apply" && !stale {
				if err := owner.Mutate(ctx, func(r EffectRepositories) error {
					item, err := r.ScoringRequests.GetByID(ctx, f.tenant.String(), lease.Identity.ResourceID.String())
					if err != nil {
						return err
					}
					switch name {
					case "failed", "pending":
						item.Status = scoring.RequestStatus(name)
					case "reference":
						item.ProviderReference = "manual-override"
					case "different_completed":
						item.Status = scoring.RequestStatusCompleted
						item.ResponseJSON = json.RawMessage(`{"manual":true}`)
					}
					_, err = r.ScoringRequests.Update(ctx, item)
					return err
				}); err != nil {
					t.Fatal(err)
				}
			}
			// The local receiver opts into interpolation. Exercise the actual
			// fenced owner update with that protocol, including JSON column typing.
			pool, err := mysqlstore.Open(ctx, mysqlstore.Config{Address: os.Getenv("DECISION_MYSQL_TEST_ADDRESS"), Database: f.database, User: "root", Password: "isolated-test-root-only", AllowInsecureTransport: true, InterpolateParameters: true, MaxOpen: 2, MaxIdle: 1, ConnectTimeout: time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { pool.Close() })
			owner, err = NewEffectStore(pool, f.database, f.tenant, owner.configuration.maxRows, owner.configuration.maxBytes, owner.configuration.authorize, owner.screening)
			if err != nil {
				t.Fatal(err)
			}
			result := json.RawMessage(`{"backend":"local-development-scoring","score":15}`)
			err = owner.ApplyScoringResult(ctx, intent.Request, reference, result)
			if name != "apply" {
				if !errors.Is(err, ErrEffectRetryState) {
					t.Fatal("stale provider result applied", err)
				}
				var status string
				schema, _ := mysqlstore.Identifier(f.database)
				expectedStatus := "pending"
				if name == "failed" {
					expectedStatus = "failed"
				}
				if name == "reference" {
					expectedStatus = "sent"
				}
				if name == "different_completed" {
					expectedStatus = "completed"
				}
				if err := f.db.QueryRow("SELECT status FROM "+schema+".decision_engine_scoring_requests WHERE id=?", mysqlstore.UUID(lease.Identity.ResourceID)).Scan(&status); err != nil || status != expectedStatus {
					t.Fatal(status, err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if err := owner.ApplyScoringResult(ctx, intent.Request, reference, result); err != nil {
					t.Fatal("idempotent result replay failed", err)
				}
			}
		})
	}
}
