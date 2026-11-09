package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/mysqlidentity"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	R "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqlruntime"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	store "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/mysqlpublication"
	"github.com/google/uuid"
	"os"
	"time"
)

// Explicit operator acceptance fixture. It uses the installed owners, real
// identity and cancellation contracts; it never synthesizes provider success.
// Disabled public configuration endpoints and dispatchers stay disabled.
func localEffectFixture(ctx context.Context, db *sql.DB, tenant uuid.UUID) error {
	raw := os.Getenv("LOCAL_DECISION_FIXTURE_DECISION")
	if raw == "" {
		return nil
	}
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return store.ErrAdministrationInput
	}
	credential, err := R.LocalReviewCredential()
	if err != nil {
		return err
	}
	verifier, err := mysqlidentity.NewVerifier(db, "fraud")
	if err != nil {
		return err
	}
	session, err := verifier.Resolve(ctx, credential)
	if err != nil {
		return err
	}
	if session.TenantID != tenant {
		return store.ErrDecisionAuthorizationDenied
	}
	authorize := func(ctx context.Context, tx *sql.Tx, t uuid.UUID, write bool) error {
		if t != tenant {
			return store.ErrDecisionAuthorizationDenied
		}
		action := "decision.workflow.read"
		if write {
			action = "decision.workflow.author"
		}
		var allowed bool
		if err := tx.QueryRowContext(ctx, "SELECT fraud.identity_authorize_decision_v1(?,?,?,?,?,?)", mysqlstore.UUID(session.ActorID), mysqlstore.UUID(t), mysqlstore.UUID(session.SessionID), session.MembershipVersion, action, write).Scan(&allowed); err != nil {
			return err
		}
		if !allowed {
			return store.ErrDecisionAuthorizationDenied
		}
		return nil
	}
	history, err := store.NewDecisionStore(db, "fraud", tenant, 500, 4<<20, authorize)
	if err != nil {
		return err
	}
	var d decision.Decision
	if err := history.ReadSnapshot(ctx, func(r store.DecisionRepositories) error {
		d, err = r.Decisions.GetByID(ctx, tenant.String(), id.String())
		return err
	}); err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	configuration, err := store.NewConfigurationStore(db, "fraud", tenant, 500, 4<<20, func(ctx context.Context, tx *sql.Tx, t uuid.UUID, _ store.AuthoringAction) error {
		return authorize(ctx, tx, t, true)
	})
	if err != nil {
		return err
	}
	screen := screening.Config{ID: uuid.NewString(), TenantID: tenant.String(), ScenarioID: d.ScenarioID, Name: "Local endpoint acceptance fixture", Provider: "local-development", ConfigJSON: []byte(`{}`), AllowedOutcomes: []string{"review"}, CreatedAt: now, UpdatedAt: now}
	score := scoring.Config{ID: uuid.NewString(), TenantID: tenant.String(), ScenarioID: d.ScenarioID, Name: screen.Name, RulesetRef: "local-acceptance", ConfigJSON: []byte(`{}`), AllowedOutcomes: []string{"review"}, CreatedAt: now, UpdatedAt: now}
	if err := configuration.Mutate(ctx, func(r store.ConfigurationRepositories) error {
		if _, err := r.ScreeningConfigs.Create(ctx, screen); err != nil {
			return err
		}
		_, err := r.ScoringConfigs.Create(ctx, score)
		return err
	}); err != nil {
		return err
	}
	policies := map[ports.JobKind]store.EffectQueuePolicy{}
	for _, kind := range []ports.JobKind{ports.JobWorkflow, ports.JobScreening, ports.JobScoring, ports.JobOutbox} {
		policies[kind] = store.EffectQueuePolicy{Name: "local_decision_" + string(kind), MaxAttempts: 3}
	}
	effects, err := store.NewQueuedEffectStore(db, "fraud", tenant, 500, 4<<20, authorize, nil, policies)
	if err != nil {
		return err
	}
	legacy := screening.Execution{ID: uuid.NewString(), TenantID: tenant.String(), DecisionID: id.String(), ScenarioID: d.ScenarioID, ConfigID: screen.ID, Status: screening.ExecutionStatusPending, RequestJSON: []byte(`{"fixture":"local-acceptance"}`), CreatedAt: now, UpdatedAt: now}
	linked := legacy
	linked.ID = uuid.NewString()
	scoreRequest := scoring.Request{ID: uuid.NewString(), TenantID: tenant.String(), DecisionID: id.String(), ScenarioID: d.ScenarioID, ConfigID: score.ID, Status: scoring.RequestStatusPending, RequestJSON: legacy.RequestJSON, CreatedAt: now, UpdatedAt: now}
	if err := effects.Mutate(ctx, func(r store.EffectRepositories) error {
		if _, err := r.ScreeningExecutions.CreateMany(ctx, []screening.Execution{legacy, linked}); err != nil {
			return err
		}
		_, err := r.ScoringRequests.CreateMany(ctx, []scoring.Request{scoreRequest})
		return err
	}); err != nil {
		return err
	}
	// Cancel exactly these fixtures before testing explicit retry. This leaves
	// durable cancellation receipts without dispatching any external operation.
	for _, item := range []struct {
		kind     ports.JobKind
		resource string
	}{{ports.JobScreening, legacy.ID}, {ports.JobScreening, linked.ID}, {ports.JobScoring, scoreRequest.ID}} {
		retry, err := store.NewEffectRetryStore(db, "fraud", tenant, item.kind, policies[item.kind], 3, func(ctx context.Context, tx *sql.Tx, t uuid.UUID, _ ports.JobKind, _ store.EffectRetryAction) error {
			return authorize(ctx, tx, t, true)
		})
		if err != nil {
			return err
		}
		if _, err := retry.Cancel(ctx, mysqljobs.Identity{Version: 1, TenantID: tenant, ResourceID: uuid.MustParse(item.resource), Kind: item.kind, DomainAttempt: 1}, "local_acceptance_fixture"); err != nil {
			return err
		}
	}
	sourceOwner, err := mysqlpublication.New(db, "fraud", tenant, authorize)
	if err != nil {
		return err
	}
	intake, err := mysqlpublication.NewIntake(sourceOwner, []string{"local-development"})
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{"execution_id": linked.ID, "request": map[string]any{"provider": "local-development", "decision_id": id.String(), "scenario_id": d.ScenarioID, "screening_config_id": screen.ID, "idempotency_key": linked.ID, "object_type": d.ObjectType, "object_id": d.ObjectID, "queries": []any{map[string]any{"name": "Local acceptance fixture"}}}})
	if err != nil {
		return err
	}
	keyJSON, _ := json.Marshal(struct {
		Kind                             string
		TenantID, ResourceID             uuid.UUID
		DomainAttempt, RedriveGeneration int
	}{string(ports.JobScreening), tenant, uuid.MustParse(linked.ID), 1, 0})
	keyHash := sha256.Sum256(keyJSON)
	payloadHash := sha256.Sum256(payload)
	receipt, err := intake.Deliver(ctx, mysqlpublication.IntakeRequest{Key: "decision-v1-" + hex.EncodeToString(keyHash[:]), PayloadSHA256: hex.EncodeToString(payloadHash[:]), TenantID: tenant, ExecutionID: uuid.MustParse(linked.ID), DomainAttempt: 1, Payload: payload})
	if err != nil {
		return err
	}
	if _, err := sourceOwner.Fail(ctx, receipt.ScreeningID, 1, "Explicit local acceptance failure; no provider called"); err != nil {
		return err
	}
	result, _ := json.Marshal(map[string]any{"screening_id": legacy.ID, "linked_execution_id": linked.ID, "source_id": receipt.ScreeningID, "screening_config_id": screen.ID, "scoring_id": scoreRequest.ID, "decision_id": id, "scenario_id": d.ScenarioID})
	fmt.Println("LOCAL_EFFECT_FIXTURE=" + string(result))
	return nil
}
