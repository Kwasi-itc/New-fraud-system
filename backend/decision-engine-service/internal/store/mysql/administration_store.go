package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/snooze"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var ErrAdministrationInput = errors.New("invalid decision administration input")

// AdministrationStore owns local workflow and comparison metadata. It does not
// dispatch effects, evaluate comparisons, or grant foreign-owner permission.
type AdministrationStore struct {
	configuration *PlatformStore
	authorize     AuthoringAuthorizer
	ids           ports.IDGenerator
	clock         ports.Clock
	inboxes       InboxContract
	tags          TagContract
}

func NewAdministrationStore(db *sql.DB, database string, tenant uuid.UUID, rows, bytes int, authorize AuthoringAuthorizer, session *TrustedDecisionSession, ids ports.IDGenerator, clock ports.Clock) (*AdministrationStore, error) {
	if authorize == nil || ids == nil || clock == nil {
		return nil, ErrOwnerContractUnavailable
	}
	owner, e := NewAuthoringStore(db, database, tenant, rows, bytes, authorize)
	if e != nil {
		return nil, e
	}
	s := &AdministrationStore{configuration: owner.configuration, authorize: authorize, ids: ids, clock: clock}
	if session != nil {
		if session.TenantID != tenant {
			return nil, ErrTenantScope
		}
		s.inboxes, e = NewInboxContract(database, *session)
		if e != nil {
			return nil, e
		}
		s.tags, e = NewTagContract(database, *session)
		if e != nil {
			return nil, e
		}
	}
	return s, nil
}
func (s *AdministrationStore) MatchesExecutionStore(e *ExecutionStore) bool {
	return s != nil && (&DecisionStore{s.configuration}).MatchesExecutionStore(e)
}
func adminRun[T any](ctx context.Context, s *AdministrationStore, write bool, action AuthoringAction, fn func(authoringRepository) (T, error)) (out T, err error) {
	if s == nil || s.configuration == nil {
		return out, ErrOwnerContractUnavailable
	}
	c := s.configuration
	run := mysqlstore.Snapshot
	if write {
		run = mysqlstore.Mutation
	}
	err = run(ctx, c.db, func(tx *sql.Tx) error {
		if e := s.authorize(ctx, tx, c.tenant, action); e != nil {
			return e
		}
		out, err = fn(authoringRepository{platformRepository: platformRepository{tx, c.schema, c.tenant, c.maxRows, c.maxBytes, write}, workflowGuards: map[uuid.UUID]bool{}})
		return err
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return out, nil
}
func (s *AdministrationStore) now() time.Time { return s.clock.Now().UTC().Truncate(time.Microsecond) }
func (s *AdministrationStore) identity() (string, error) {
	id := s.ids.New()
	if id == uuid.Nil {
		return "", ErrTenantScope
	}
	return id.String(), nil
}
func workflowRepo(r authoringRepository) WorkflowRepository {
	return WorkflowRepository{configurationRepository{r, workflowConfiguration}}
}

// Authorize all old/new destinations before owner locks. Existing configurations
// need not remain active to be disabled/deleted. Tags are checked only on saves.
func (s *AdministrationStore) workflowDestinations(ctx context.Context, r authoringRepository, items []workflow.Definition, write bool) error {
	ids := map[uuid.UUID]bool{}
	for _, v := range items {
		if !workflow.IsCaseAction(v.ActionType) {
			continue
		}
		var cfg struct {
			InboxID string `json:"inbox_id"`
		}
		if json.Unmarshal(v.ActionConfig, &cfg) != nil {
			return ErrOwnerRelationshipConflict
		}
		id, e := uuid.Parse(cfg.InboxID)
		if e != nil || id == uuid.Nil {
			return ErrOwnerRelationshipConflict
		}
		ids[id] = true
	}
	if len(ids) == 0 {
		return nil
	}
	list := make([]uuid.UUID, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	return s.inboxes.AuthorizeBatch(ctx, r.tx, r.tenant, list, write, false)
}
func (s *AdministrationStore) Workflows(ctx context.Context, parent uuid.UUID) ([]workflow.Definition, error) {
	return adminRun(ctx, s, false, AuthoringWorkflowRead, func(r authoringRepository) ([]workflow.Definition, error) {
		t := r.tenant.String()
		if _, e := (ScenarioRepository{r}).GetByID(ctx, t, parent.String()); e != nil {
			return nil, e
		}
		items, e := workflowRepo(r).ListByScenario(ctx, t, parent.String())
		if e != nil {
			return nil, e
		}
		if e = s.workflowDestinations(ctx, r, items, false); e != nil {
			return nil, e
		}
		return items, nil
	})
}
func (s *AdministrationStore) Workflow(ctx context.Context, parent, id uuid.UUID) (workflow.Definition, error) {
	return adminRun(ctx, s, false, AuthoringWorkflowRead, func(r authoringRepository) (workflow.Definition, error) {
		v, e := workflowRepo(r).GetByID(ctx, r.tenant.String(), parent.String(), id.String())
		if e != nil {
			return v, e
		}
		return v, s.workflowDestinations(ctx, r, []workflow.Definition{v}, false)
	})
}

func (s *AdministrationStore) SaveWorkflow(ctx context.Context, parent, id uuid.UUID, fields workflow.Definition) (workflow.Definition, error) {
	return adminRun(ctx, s, true, AuthoringWorkflowWrite, func(r authoringRepository) (workflow.Definition, error) {
		t, p := r.tenant.String(), parent.String()
		repo := workflowRepo(r)
		create := id == uuid.Nil
		v := fields
		v.TenantID = t
		v.ScenarioID = p
		v.UpdatedAt = s.now()
		var old workflow.Definition
		if create {
			if _, e := (ScenarioRepository{r}).GetByID(ctx, t, p); e != nil {
				return v, e
			}
			var e error
			v.ID, e = s.identity()
			if e != nil {
				return v, e
			}
			v.CreatedAt = v.UpdatedAt
		} else {
			var e error
			old, e = repo.GetByID(ctx, t, p, id.String())
			if e != nil {
				return v, e
			}
			v.ID = old.ID
			v.CreatedAt = old.CreatedAt
			v.DisplayOrder = old.DisplayOrder
		}
		if e := v.Validate(); e != nil {
			return v, errors.Join(ErrAdministrationInput, e)
		}
		if len(v.AllowedOutcomes) > r.maxRows {
			return v, ErrProjectionLimit
		}
		if e := r.scope(t, append([]string{v.Name, v.Description, string(v.ActionConfig)}, v.AllowedOutcomes...)...); e != nil {
			return v, errors.Join(ErrAdministrationInput, e)
		}
		if e := s.workflowDestinations(ctx, r, []workflow.Definition{old, v}, true); e != nil {
			return v, e
		}
		if workflow.IsCaseAction(v.ActionType) {
			var cfg struct {
				InboxID string   `json:"inbox_id"`
				TagIDs  []string `json:"tag_ids"`
			}
			if e := json.Unmarshal(v.ActionConfig, &cfg); e != nil {
				return v, e
			}
			if e := s.inboxes.Authorize(ctx, r.tx, r.tenant, uuid.MustParse(cfg.InboxID), true, v.Active); e != nil {
				return v, e
			}
			tags := make([]uuid.UUID, len(cfg.TagIDs))
			for i, id := range cfg.TagIDs {
				tags[i] = uuid.MustParse(id)
			}
			if e := s.tags.Authorize(ctx, r.tx, r.tenant, tags, true, v.Active); e != nil {
				return v, e
			}
		}
		if e := r.lockWorkflowAuthoring(ctx, t, p); e != nil {
			return v, e
		}
		if create {
			var last sql.NullInt64
			if e := r.tx.QueryRowContext(ctx, "SELECT MAX(display_order) FROM "+r.schema+".decision_engine_workflows WHERE tenant_id=? AND scenario_id=?", mysqlstore.UUID(r.tenant), mysqlstore.UUID(parent)).Scan(&last); e != nil {
				return v, e
			}
			if last.Valid {
				if last.Int64 >= math.MaxInt32 {
					return v, ErrProjectionLimit
				}
				v.DisplayOrder = int(last.Int64) + 1
			} else {
				v.DisplayOrder = 0
			}
			return repo.Create(ctx, v)
		}
		current, e := repo.GetByID(ctx, t, p, id.String())
		if e != nil {
			return v, e
		}
		if !reflect.DeepEqual(current, old) {
			return v, ErrOwnerRelationshipConflict
		}
		return repo.Update(ctx, v)
	})
}
func (s *AdministrationStore) DeleteWorkflow(ctx context.Context, parent, id uuid.UUID) error {
	_, e := adminRun(ctx, s, true, AuthoringWorkflowWrite, func(r authoringRepository) (struct{}, error) {
		repo := workflowRepo(r)
		t, p := r.tenant.String(), parent.String()
		v, e := repo.GetByID(ctx, t, p, id.String())
		if e != nil {
			return struct{}{}, e
		}
		if e = s.workflowDestinations(ctx, r, []workflow.Definition{v}, true); e != nil {
			return struct{}{}, e
		}
		if e = r.lockWorkflowAuthoring(ctx, t, p); e != nil {
			return struct{}{}, e
		}
		current, e := repo.GetByID(ctx, t, p, id.String())
		if e != nil {
			return struct{}{}, e
		}
		if !reflect.DeepEqual(v, current) {
			return struct{}{}, ErrOwnerRelationshipConflict
		}
		return struct{}{}, repo.Delete(ctx, t, p, id.String())
	})
	return e
}
func (s *AdministrationStore) ReorderWorkflows(ctx context.Context, parent uuid.UUID, ids []string) error {
	ids = append([]string(nil), ids...)
	_, e := adminRun(ctx, s, true, AuthoringWorkflowWrite, func(r authoringRepository) (struct{}, error) {
		repo := workflowRepo(r)
		t, p := r.tenant.String(), parent.String()
		if _, e := (ScenarioRepository{r}).GetByID(ctx, t, p); e != nil {
			return struct{}{}, e
		}
		items, e := repo.ListByScenario(ctx, t, p)
		if e != nil {
			return struct{}{}, e
		}
		if len(ids) != len(items) {
			return struct{}{}, ErrAdministrationInput
		}
		expected := map[string]bool{}
		for _, v := range items {
			expected[v.ID] = true
		}
		for i, id := range ids {
			u, e := uuid.Parse(id)
			if e != nil || !expected[u.String()] {
				return struct{}{}, ErrAdministrationInput
			}
			ids[i] = u.String()
			delete(expected, u.String())
		}
		if e = s.workflowDestinations(ctx, r, items, true); e != nil {
			return struct{}{}, e
		}
		if e = r.lockWorkflowAuthoring(ctx, t, p); e != nil {
			return struct{}{}, e
		}
		current, e := repo.ListByScenario(ctx, t, p)
		if e != nil {
			return struct{}{}, e
		}
		if !reflect.DeepEqual(items, current) {
			return struct{}{}, ErrOwnerRelationshipConflict
		}
		return struct{}{}, repo.Reorder(ctx, t, p, ids, s.now())
	})
	return e
}

func (s *AdministrationStore) CreateTestRun(ctx context.Context, parent, phantom uuid.UUID, expires time.Time) (scenario.TestRun, error) {
	return adminRun(ctx, s, true, AuthoringWrite, func(r authoringRepository) (scenario.TestRun, error) {
		var v scenario.TestRun
		t, p := r.tenant.String(), parent.String()
		now := s.now()
		if phantom == uuid.Nil || !expires.After(now) {
			return v, ErrAdministrationInput
		}
		if _, e := (mysqlstore.DateTime{Time: expires}).Value(); e != nil {
			return v, ErrAdministrationInput
		}
		// Shared parent lock pins publication without serializing other read/creates.
		args, e := r.ids(t, p)
		if e != nil {
			return v, e
		}
		var found mysqlstore.UUID
		if e = r.tx.QueryRowContext(ctx, "SELECT id FROM "+r.schema+".decision_engine_scenarios WHERE tenant_id=? AND id=? FOR SHARE", args...).Scan(&found); e != nil {
			return v, e
		}
		sc, e := (ScenarioRepository{r}).GetByID(ctx, t, p)
		if e != nil {
			return v, e
		}
		if sc.LiveIterationID == nil || *sc.LiveIterationID == phantom.String() {
			return v, ErrAdministrationInput
		}
		if _, e = (ScenarioIterationRepository{r}).GetByID(ctx, t, p, phantom.String()); e != nil {
			return v, e
		}
		id, e := s.identity()
		if e != nil {
			return v, e
		}
		v = scenario.TestRun{ID: id, TenantID: t, ScenarioID: p, LiveIterationID: *sc.LiveIterationID, PhantomIterationID: phantom.String(), Status: scenario.TestRunStatusUp, CreatedAt: now, UpdatedAt: now, ExpiresAt: expires}
		return (TestRunRepository{r}).Create(ctx, v)
	})
}
func (s *AdministrationStore) CancelTestRun(ctx context.Context, id uuid.UUID) (scenario.TestRun, error) {
	return adminRun(ctx, s, true, AuthoringWrite, func(r authoringRepository) (scenario.TestRun, error) {
		return (TestRunRepository{r}).UpdateStatus(ctx, r.tenant.String(), id.String(), scenario.TestRunStatusDown, s.now())
	})
}
func (s *AdministrationStore) CreateSnooze(ctx context.Context, parent uuid.UUID, fields snooze.RuleSnooze) (snooze.RuleSnooze, error) {
	return adminRun(ctx, s, true, AuthoringWrite, func(r authoringRepository) (snooze.RuleSnooze, error) {
		v := fields
		v.TenantID = r.tenant.String()
		v.ScenarioID = parent.String()
		v.CreatedAt = s.now()
		id, e := s.identity()
		if e != nil {
			return v, e
		}
		v.ID = id
		if e = v.Validate(); e != nil {
			return v, errors.Join(ErrAdministrationInput, e)
		}
		if e = r.scope(v.TenantID, v.ObjectType, v.ObjectID); e != nil {
			return v, errors.Join(ErrAdministrationInput, e)
		}
		group, e := uuid.Parse(v.SnoozeGroupID)
		if e != nil || group == uuid.Nil {
			return v, ErrAdministrationInput
		}
		v.SnoozeGroupID = group.String()
		if _, e = (mysqlstore.DateTime{Time: v.ExpiresAt}).Value(); e != nil {
			return v, ErrAdministrationInput
		}
		if _, e = (ScenarioRepository{r}).GetByID(ctx, v.TenantID, v.ScenarioID); e != nil {
			return v, e
		}
		return (RuleSnoozeRepository{r}).Create(ctx, v)
	})
}
