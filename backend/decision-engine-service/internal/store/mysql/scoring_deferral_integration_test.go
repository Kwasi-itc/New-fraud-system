package mysql

import (
	"context"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestMySQLAdditionalScoringDeferral(t *testing.T) {
	f, original, _ := combinedFixture(t, true)
	ctx := context.Background()
	deferred, err := original.effects.WithoutAdditionalScoring()
	if err != nil {
		t.Fatal(err)
	}
	schema, _ := mysqlstore.Identifier(f.database)
	if err := deferred.CheckAdditionalScoringDeferred(ctx); !errors.Is(err, ErrAdditionalScoringDeferred) {
		t.Fatal("active configuration accepted", err)
	}
	if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".decision_engine_scoring_configs SET active=0 WHERE tenant_id=?", mysqlstore.UUID(f.tenant)); err != nil {
		t.Fatal(err)
	}
	if err := deferred.CheckAdditionalScoringDeferred(ctx); err != nil {
		t.Fatal(err)
	}
	unit, err := NewDecisionUnitOfWorkStore(deferred, original.executions)
	if err != nil {
		t.Fatal(err)
	}
	d := decisionInput(f.tenant, f.parent, f.iteration.ID)
	score := scoring.Request{ID: uuid.NewString(), TenantID: d.TenantID, ConfigID: f.score.ID, DecisionID: d.ID, ScenarioID: f.parent.String(), Status: scoring.RequestStatusPending, RequestJSON: []byte(`{}`), CreatedAt: d.CreatedAt, UpdatedAt: d.CreatedAt}
	// Even a caller ignoring the rejected effect cannot commit its decision.
	err = unit.RunDecisionMutation(ctx, func(s ports.DecisionMutationStore) error {
		if _, err := s.Decisions().Create(ctx, d); err != nil {
			return err
		}
		_, _ = s.ScoringRequests().CreateMany(ctx, []scoring.Request{score})
		return nil
	})
	if !errors.Is(err, ErrAdditionalScoringDeferred) {
		t.Fatal(err)
	}
	var count int
	if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".decision_engine_decisions WHERE id=?", mysqlstore.UUID(uuid.MustParse(d.ID))).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial decision committed", count, err)
	}
	// Normal rule evaluation output still persists without an external request.
	if err := unit.RunDecisionMutation(ctx, func(s ports.DecisionMutationStore) error { _, err := s.Decisions().Create(ctx, d); return err }); err != nil {
		t.Fatal(err)
	}
	// The original capability remains unchanged; pre-existing work must block
	// deferred activation, not be deleted or represented as successful.
	if err := original.effects.Mutate(ctx, func(r EffectRepositories) error {
		_, err := r.ScoringRequests.CreateMany(ctx, []scoring.Request{score})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := deferred.CheckAdditionalScoringDeferred(ctx); !errors.Is(err, ErrAdditionalScoringDeferred) {
		t.Fatal("unfinished request accepted", err)
	}
	if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".decision_engine_scoring_requests SET status='completed' WHERE id=?", mysqlstore.UUID(uuid.MustParse(score.ID))); err != nil {
		t.Fatal(err)
	}
	if err := deferred.CheckAdditionalScoringDeferred(ctx); !errors.Is(err, ErrAdditionalScoringDeferred) {
		t.Fatal("live transport abandoned", err)
	}
	if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".decision_engine_effect_job_deliveries SET state='cancelled',finished_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND kind='scoring_request'", mysqlstore.UUID(f.tenant)); err != nil {
		t.Fatal(err)
	}
	if err := deferred.CheckAdditionalScoringDeferred(ctx); err != nil {
		t.Fatal("retained completed history blocked activation", err)
	}
	for name, mutate := range map[string]func(ports.DecisionMutationStore) error{
		"status update": func(s ports.DecisionMutationStore) error {
			return s.ScoringRequests().UpdateStatus(ctx, score.ID, scoring.RequestStatusPending)
		},
		"request update": func(s ports.DecisionMutationStore) error {
			_, err := s.ScoringRequests().Update(ctx, score)
			return err
		},
		"enqueue": func(s ports.DecisionMutationStore) error {
			return s.Enqueue(ctx, ports.JobRequest{Kind: ports.JobScoring, TenantID: d.TenantID, ResourceID: score.ID})
		},
	} {
		t.Run(name, func(t *testing.T) {
			marker := decisionInput(f.tenant, f.parent, f.iteration.ID)
			err := unit.RunDecisionMutation(ctx, func(s ports.DecisionMutationStore) error {
				if _, err := s.Decisions().Create(ctx, marker); err != nil {
					return err
				}
				_ = mutate(s)
				return nil
			})
			if !errors.Is(err, ErrAdditionalScoringDeferred) {
				t.Fatal("ignored deferred mutation committed", err)
			}
			if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".decision_engine_decisions WHERE id=?", mysqlstore.UUID(uuid.MustParse(marker.ID))).Scan(&count); err != nil || count != 0 {
				t.Fatal("marker committed", count, err)
			}
		})
	}
}
