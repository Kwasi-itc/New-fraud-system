package mysql

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"math"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var ErrIterationReplayConflict = errors.New("iteration creation operation reused with different input")
var ErrIterationVersionExhausted = errors.New("scenario iteration version exhausted")

// DraftInput has stable caller-allocated identities. Server time and the allocated
// version are deliberately not request identity. Reuse the SAME input/operation
// after an uncertain commit; do not allocate a new ID or retry blindly.
type DraftInput struct {
	ID                           uuid.UUID
	ScenarioID                   uuid.UUID
	TriggerFormula               json.RawMessage
	ScoreReviewThreshold         *int
	ScoreBlockAndReviewThreshold *int
	ScoreDeclineThreshold        *int
	Schedule                     string
}

type IterationCreation struct {
	Iteration scenario.Iteration
	Replay    bool
}

// IterationCreator is a staged, narrow API, not a partial TransactionManager.
// Authorization must use trusted identity and the same local transaction.
// It does NOT authorize via a tenant parameter alone or perform publication.
// The authoring service must perform model/AST validation before invoking this
// storage component; its validation here covers storage and threshold contracts.
type IterationCreator struct {
	db        *sql.DB
	schema    string
	tenant    uuid.UUID
	authorize PlatformAuthorizer
}

func NewIterationCreator(db *sql.DB, database string, tenant uuid.UUID, authorize PlatformAuthorizer) (*IterationCreator, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if db == nil || tenant == uuid.Nil || authorize == nil {
		return nil, errors.New("pool, tenant and trusted iteration authorizer required")
	}
	return &IterationCreator{db, schema, tenant, authorize}, nil
}

// CreateDraft commits the draft, counter and immutable creation receipt together.
// Counter locking is per scenario, never the mutable scenario parent or tenant.
// Ordinary authored-content changes will not need this counter. No automatic
// retry: a CommitError must be reconciled by the same operation and input.
func (s *IterationCreator) CreateDraft(ctx context.Context, operation uuid.UUID, input DraftInput) (IterationCreation, error) {
	if s == nil || s.db == nil || s.authorize == nil {
		return IterationCreation{}, errors.New("initialized iteration creator required")
	}
	body, err := validateDraft(s.tenant, operation, input)
	if err != nil {
		return IterationCreation{}, err
	}
	hash := sha256.Sum256(body)
	var result IterationCreation
	err = mysqlstore.Mutation(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.authorize(ctx, tx, s.tenant, true); err != nil {
			return err
		}
		tenant, parent, op := mysqlstore.UUID(s.tenant), mysqlstore.UUID(input.ScenarioID), mysqlstore.UUID(operation)
		// One PK only; FK enforces a real same-tenant scenario, including first use.
		if _, err := tx.ExecContext(ctx, "INSERT INTO "+s.schema+".decision_engine_scenario_versions(tenant_id,scenario_id,last_version) VALUES(?,?,0) ON DUPLICATE KEY UPDATE last_version=last_version", tenant, parent); err != nil {
			return err
		}
		var previous mysqlstore.Int32
		if err := tx.QueryRowContext(ctx, "SELECT last_version FROM "+s.schema+".decision_engine_scenario_versions WHERE tenant_id=? AND scenario_id=? FOR UPDATE", tenant, parent).Scan(&previous); err != nil {
			return err
		}
		var storedHash, storedBody []byte
		err := tx.QueryRowContext(ctx, "SELECT request_hash,result_body FROM "+s.schema+".decision_engine_iteration_creations WHERE tenant_id=? AND scenario_id=? AND operation_id=? FOR SHARE", tenant, parent, op).Scan(&storedHash, &storedBody)
		if err == nil {
			if !bytes.Equal(hash[:], storedHash) {
				return ErrIterationReplayConflict
			}
			if len(storedBody) > 2<<20 {
				return ErrProjectionLimit
			}
			if err := json.Unmarshal(storedBody, &result.Iteration); err != nil {
				return err
			}
			if result.Iteration.TenantID != s.tenant.String() || result.Iteration.ScenarioID != input.ScenarioID.String() || result.Iteration.ID != input.ID.String() {
				return errors.New("iteration creation receipt binding invalid")
			}
			result.Replay = true
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if previous == math.MaxInt32 {
			return ErrIterationVersionExhausted
		}
		var now mysqlstore.DateTime
		if err := tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&now); err != nil {
			return err
		}
		version := previous + 1
		item := scenario.Iteration{ID: input.ID.String(), TenantID: s.tenant.String(), ScenarioID: input.ScenarioID.String(), Version: int(version), Status: scenario.IterationStatusDraft, TriggerFormula: append(json.RawMessage(nil), input.TriggerFormula...), ScoreReviewThreshold: input.ScoreReviewThreshold, ScoreBlockAndReviewThreshold: input.ScoreBlockAndReviewThreshold, ScoreDeclineThreshold: input.ScoreDeclineThreshold, Schedule: input.Schedule, CreatedAt: now.Time}
		_, err = tx.ExecContext(ctx, "INSERT INTO "+s.schema+".decision_engine_scenario_iterations(id,tenant_id,scenario_id,version,status,trigger_formula,score_review_threshold,score_block_and_review_threshold,score_decline_threshold,schedule,created_at,committed_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,NULL)", mysqlstore.UUID(input.ID), tenant, parent, version, mysqlstore.Text(string(item.Status)), mysqlstore.JSON(item.TriggerFormula), threshold(input.ScoreReviewThreshold), threshold(input.ScoreBlockAndReviewThreshold), threshold(input.ScoreDeclineThreshold), mysqlstore.Text(input.Schedule), now)
		if err != nil {
			return err
		}
		// Read the stored JSON on the same transaction, not the unnormalized
		// input. MySQL JSON formatting/number representation is storage-owned.
		var formula mysqlstore.JSON
		if err := tx.QueryRowContext(ctx, "SELECT trigger_formula FROM "+s.schema+".decision_engine_scenario_iterations WHERE tenant_id=? AND scenario_id=? AND id=?", tenant, parent, mysqlstore.UUID(input.ID)).Scan(&formula); err != nil {
			return err
		}
		item.TriggerFormula = json.RawMessage(formula)
		if _, err := tx.ExecContext(ctx, "UPDATE "+s.schema+".decision_engine_scenario_versions SET last_version=? WHERE tenant_id=? AND scenario_id=?", version, tenant, parent); err != nil {
			return err
		}
		response, err := json.Marshal(item)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO "+s.schema+".decision_engine_iteration_creations(tenant_id,scenario_id,operation_id,request_hash,result_body) VALUES(?,?,?,?,?)", tenant, parent, op, hash[:], response); err != nil {
			return err
		}
		// Use the same encoded response image for the first return and replay
		// (encoding/json compacts embedded RawMessage whitespace).
		return json.Unmarshal(response, &result.Iteration)
	})
	if err != nil {
		return IterationCreation{}, err
	}
	return result, nil
}

func threshold(value *int) mysqlstore.NullInt32 {
	if value == nil {
		return mysqlstore.NullInt32{}
	}
	return mysqlstore.NullInt32{Int32: mysqlstore.Int32(*value), Valid: true}
}

func validateDraft(tenant, operation uuid.UUID, input DraftInput) ([]byte, error) {
	if tenant == uuid.Nil || operation == uuid.Nil || input.ID == uuid.Nil || input.ScenarioID == uuid.Nil {
		return nil, ErrTenantScope
	}
	if len(input.TriggerFormula) > 1<<20 || len(input.Schedule) > 1<<20 {
		return nil, ErrProjectionLimit
	}
	if _, err := (mysqlstore.JSON(input.TriggerFormula)).Value(); err != nil {
		return nil, err
	}
	if _, err := (mysqlstore.Text(input.Schedule)).Value(); err != nil {
		return nil, err
	}
	for _, v := range []*int{input.ScoreReviewThreshold, input.ScoreBlockAndReviewThreshold, input.ScoreDeclineThreshold} {
		if v != nil && (*v < math.MinInt32 || *v > math.MaxInt32) {
			return nil, errors.New("iteration threshold outside signed INT range")
		}
	}
	item := scenario.Iteration{TenantID: tenant.String(), ScenarioID: input.ScenarioID.String(), Status: scenario.IterationStatusDraft, ScoreReviewThreshold: input.ScoreReviewThreshold, ScoreBlockAndReviewThreshold: input.ScoreBlockAndReviewThreshold, ScoreDeclineThreshold: input.ScoreDeclineThreshold}
	if err := item.Validate(); err != nil {
		return nil, err
	}
	body, err := json.Marshal(input)
	if len(body) > 1<<20 {
		return nil, ErrProjectionLimit
	}
	return body, err
}
