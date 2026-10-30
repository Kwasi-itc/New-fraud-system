package mysql

import (
	"context"
	"database/sql"
	"errors"
	"sort"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// ExecutionPreparationSnapshot is read-only owner evidence for one preparation,
// not a committed result or a held lock. Only an authorized store can construct
// it. The eventual persistence transaction MUST recheck its lease/generation.
type ExecutionPreparationSnapshot struct {
	parent    scenario.Scenario
	iteration scenario.Iteration
	rules     []scenario.Rule
	scheduled *ScheduledExecutionRecord
}

func (p ExecutionPreparationSnapshot) Scenario() scenario.Scenario {
	v := p.parent
	if v.LiveIterationID != nil {
		s := *v.LiveIterationID
		v.LiveIterationID = &s
	}
	return v
}
func (p ExecutionPreparationSnapshot) Iteration() scenario.Iteration {
	v := p.iteration
	v.TriggerFormula = append([]byte(nil), v.TriggerFormula...)
	if v.ScoreReviewThreshold != nil {
		x := *v.ScoreReviewThreshold
		v.ScoreReviewThreshold = &x
	}
	if v.ScoreBlockAndReviewThreshold != nil {
		x := *v.ScoreBlockAndReviewThreshold
		v.ScoreBlockAndReviewThreshold = &x
	}
	if v.ScoreDeclineThreshold != nil {
		x := *v.ScoreDeclineThreshold
		v.ScoreDeclineThreshold = &x
	}
	if v.CommittedAt != nil {
		x := *v.CommittedAt
		v.CommittedAt = &x
	}
	return v
}
func (p ExecutionPreparationSnapshot) Rules() []scenario.Rule {
	out := append([]scenario.Rule{}, p.rules...)
	for i := range out {
		out[i].Formula = append([]byte(nil), out[i].Formula...)
		if out[i].SnoozeGroupID != nil {
			x := *out[i].SnoozeGroupID
			out[i].SnoozeGroupID = &x
		}
	}
	return out
}
func (p ExecutionPreparationSnapshot) Scheduled() (ScheduledExecutionRecord, error) {
	if p.scheduled == nil {
		return ScheduledExecutionRecord{}, ErrExecutionState
	}
	v := *p.scheduled
	v.RequestBody = append([]byte(nil), v.RequestBody...)
	return v, nil
}

// PreparationForRequest resolves CURRENT live selection once on an authorized,
// committed-version read. It is preparation evidence only; admission must fence
// the live/model bindings again before creating the durable owner (later step).
func (s *ExecutionStore) PreparationForRequest(ctx context.Context, parent uuid.UUID) (ExecutionPreparationSnapshot, error) {
	return s.preparationSnapshot(ctx, ExecutionRequest, parent, nil)
}

func (s *ExecutionStore) PreparationForScheduleAdmin(ctx context.Context, parent uuid.UUID) (ExecutionPreparationSnapshot, error) {
	return s.preparationSnapshot(ctx, ExecutionScheduleAdmin, parent, nil)
}

// PreparationForScheduled reads the actual scheduled owner and its immutable
// committed iteration/rules in one short authorized operation. Current live selection
// is intentionally irrelevant. Token/expiry are rechecked by completion; here
// the owner must already be running at this attempt and generation.
func (s *ExecutionStore) PreparationForScheduled(ctx context.Context, identity mysqljobs.Identity) (ExecutionPreparationSnapshot, error) {
	if identity.Validate() != nil || identity.Kind != ports.JobScheduled {
		return ExecutionPreparationSnapshot{}, ErrExecutionState
	}
	return s.preparationSnapshot(ctx, ExecutionRun, uuid.Nil, &identity)
}
func (s *ExecutionStore) preparationSnapshot(ctx context.Context, action ExecutionAction, parent uuid.UUID, identity *mysqljobs.Identity) (ExecutionPreparationSnapshot, error) {
	if s == nil || s.configuration == nil || s.authorize == nil {
		return ExecutionPreparationSnapshot{}, ErrDecisionAuthorizationUnavailable
	}
	c := s.configuration
	if identity != nil && identity.TenantID != c.tenant {
		return ExecutionPreparationSnapshot{}, ErrTenantScope
	}
	var out ExecutionPreparationSnapshot
	// Run/request authority may take identity locks. Use the standard short RC
	// transaction, not the strictly nonlocking/read-only Snapshot helper. The
	// selected committed iteration/rules are immutable; this does not claim a
	// global snapshot of mutable owner data or later historical evaluation reads.
	err := mysqlstore.Mutation(ctx, c.db, func(tx *sql.Tx) error {
		if err := s.authorize(ctx, tx, c.tenant, action); err != nil {
			return err
		}
		r := authoringRepository{platformRepository: platformRepository{tx, c.schema, c.tenant, c.maxRows, c.maxBytes, false}}
		iteration := ""
		if identity != nil {
			x := s.repositories(tx, action, nil)
			owner, err := x.Scheduled.get(ctx, identity.ResourceID)
			if err != nil {
				return err
			}
			if owner.Status != execution.StatusRunning || owner.Generation != identity.RedriveGeneration || owner.AttemptCount != identity.DomainAttempt {
				return ErrExecutionState
			}
			out.scheduled = &owner
			parent = uuid.MustParse(owner.ScenarioID)
			iteration = owner.ScenarioIterationID
		}
		var err error
		out.parent, err = (ScenarioRepository{r}).GetByID(ctx, c.tenant.String(), parent.String())
		if err != nil {
			return err
		}
		if identity == nil {
			if out.parent.LiveIterationID == nil {
				return ErrExecutionState
			}
			iteration = *out.parent.LiveIterationID
		}
		out.iteration, err = (ScenarioIterationRepository{r}).GetByID(ctx, c.tenant.String(), parent.String(), iteration)
		if err != nil {
			return err
		}
		if out.iteration.Status != scenario.IterationStatusCommitted || out.iteration.CommittedAt == nil {
			return ErrCommittedIteration
		}
		out.rules, err = (RuleRepository{r}).ListByIteration(ctx, c.tenant.String(), parent.String(), iteration)
		return err
	})
	if err != nil {
		return ExecutionPreparationSnapshot{}, err
	}
	return out, nil
}

// ValidateExecutionPreparation checks the trigger table AND each aggregate table
// with the real owner registry/compiler, even when the trigger/rules contain no
// aggregate. A fresh model must have been resolved before this short registry
// transaction; no network/provider call is made under these shared gate locks.
// Queries later recheck gates individually; this is not a global data snapshot.
func (r TenantDataReader) ValidateExecutionPreparation(ctx context.Context, snapshot ExecutionPreparationSnapshot, model ports.TenantModel) error {
	c := r.configuration
	if c == nil || snapshot.parent.TenantID != c.tenant.String() || snapshot.iteration.TenantID != c.tenant.String() {
		return ErrTenantScope
	}
	queries, err := aggregatePublicationQueries(ctx, PublicationSnapshot{Scenario: snapshot.parent, Iteration: snapshot.iteration, Rules: snapshot.rules}, model, c.maxRows, c.maxBytes)
	if err != nil {
		return &ports.LocalTenantReadError{Cause: err}
	}
	names := map[string]bool{snapshot.parent.TriggerObjectType: true}
	return r.validateExecutionQueries(ctx, names, queries, model)
}

func (r TenantDataReader) validateExecutionQueries(ctx context.Context, names map[string]bool, queries []ports.AggregateQuery, model ports.TenantModel) error {
	c := r.configuration
	guard, err := r.executionRegistryGuard(names, queries, model)
	if err != nil {
		return err
	}
	err = mysqlstore.Mutation(ctx, c.db, func(tx *sql.Tx) error {
		if err := c.authorize(ctx, tx, c.tenant, false); err != nil {
			return err
		}
		return guard(ctx, tx)
	})
	if err != nil {
		return errors.Join(ErrPublishedModelUnavailable, err)
	}
	return nil
}

// Prepare once before owner write locks; the returned private guard performs
// only registered binding checks/SQL compilation on the caller's transaction.
func (r TenantDataReader) executionRegistryGuard(names map[string]bool, queries []ports.AggregateQuery, model ports.TenantModel) (func(context.Context, *sql.Tx) error, error) {
	c := r.configuration
	if c == nil {
		return nil, ErrPublishedModelUnavailable
	}
	for _, q := range queries {
		names[q.ObjectType] = true
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		table, ok := model.Tables[name]
		id, e := uuid.Parse(table.ID)
		version, v := uuid.Parse(table.PublishedVersion)
		if !ok || table.Archived || table.Name != name || e != nil || v != nil || id == uuid.Nil || version == uuid.Nil {
			return nil, ErrPublishedModelUnavailable
		}
		ordered = append(ordered, name)
	}
	sort.Slice(ordered, func(i, j int) bool { return model.Tables[ordered[i]].ID < model.Tables[ordered[j]].ID })
	return func(ctx context.Context, tx *sql.Tx) error {
		repo := authoringRepository{platformRepository: platformRepository{tx, c.schema, c.tenant, c.maxRows, c.maxBytes, false}}
		for _, name := range ordered {
			table := model.Tables[name]
			bound, err := repo.registry(ctx, uuid.MustParse(table.ID), uuid.MustParse(table.PublishedVersion), model, table)
			if err != nil {
				return err
			}
			for _, q := range queries {
				if q.ObjectType == name {
					if _, _, err := bound.compileAggregate(q, c.maxBytes); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}, nil
}

// MatchesExecutionPreparationScope prevents a target coordinator from mixing
// registry/owner pools, logical databases, tenants or projection budgets.
func (r TenantDataReader) MatchesExecutionPreparationScope(s *ExecutionStore) bool {
	if r.configuration == nil || s == nil || s.configuration == nil {
		return false
	}
	a, b := r.configuration, s.configuration
	return a.db == b.db && a.schema == b.schema && a.tenant == b.tenant && a.maxRows == b.maxRows && a.maxBytes == b.maxBytes
}

func (s *ExecutionStore) ExecutionPreparationScope() (uuid.UUID, int, int, error) {
	if s == nil || s.configuration == nil || s.authorize == nil {
		return uuid.Nil, 0, 0, ErrDecisionAuthorizationUnavailable
	}
	c := s.configuration
	return c.tenant, c.maxRows, c.maxBytes, nil
}

// ResolveExecutionModel performs only provider IO, before any registry or
// persistence transaction. Target coordinators must detach/bound its result.
func (r TenantDataReader) ResolveExecutionModel(ctx context.Context) (ports.TenantModel, error) {
	if r.configuration == nil || r.models == nil {
		return ports.TenantModel{}, ErrPublishedModelUnavailable
	}
	return r.models.GetTenantModel(ctx, r.configuration.tenant.String())
}

// ValidateExecutionObjectModel verifies real published registry bindings for
// payload-only admission (no scenario selected yet). It performs no record read
// and does not claim to validate not-yet-selected scenario formulas.
func (r TenantDataReader) ValidateExecutionObjectModel(ctx context.Context, model ports.TenantModel, objectType string) error {
	if r.configuration == nil {
		return ErrPublishedModelUnavailable
	}
	scoped := r.WithModel(r.configuration.tenant.String(), model).(TenantDataReader)
	return scoped.read(ctx, r.configuration.tenant.String(), objectType, func(authoringRepository, registryTable) error { return nil })
}
