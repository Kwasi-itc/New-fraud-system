package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// BrowserWorkflowStore stages browser editing and audited copying. Runtime
// activation remains pending; no real auth/case provider is supplied here.
// Auth/inbox/tag providers are mandatory and run locally on the SAME transaction.
// No case tables are mutated and no network call is made under these locks.
type BrowserWorkflowStore struct {
	configuration *PlatformStore
	session       TrustedDecisionSession
	authorize     AuthoringAuthorizer
	inboxes       InboxContract
	tags          TagContract
}

func browserAuthoringError(err error) error {
	if errors.Is(err, ErrDecisionAuthorizationDenied) {
		return errors.Join(workflow.ErrAuthoringDenied, err)
	}
	return err
}

func NewBrowserWorkflowStore(db *sql.DB, database string, session TrustedDecisionSession, maxRows, maxBytes int) (*BrowserWorkflowStore, error) {
	authorize, err := SessionAuthorizer(database, session)
	if err != nil {
		return nil, err
	}
	c, err := NewPlatformStore(db, database, session.TenantID, maxRows, maxBytes, func(ctx context.Context, tx *sql.Tx, t uuid.UUID, write bool) error {
		action := AuthoringWorkflowRead
		if write {
			action = AuthoringWorkflowWrite
		}
		return authorize(ctx, tx, t, action)
	})
	if err != nil {
		return nil, err
	}
	inboxes, err := NewInboxContract(database, session)
	if err != nil {
		return nil, err
	}
	tags, err := NewTagContract(database, session)
	if err != nil {
		return nil, err
	}
	return &BrowserWorkflowStore{c, session, authorize, inboxes, tags}, nil
}

func (s *BrowserWorkflowStore) List(ctx context.Context, tenant, scenario string, actor workflow.AuthoringActor) (workflow.AuthoringPage, error) {
	if s == nil || s.configuration == nil || s.authorize == nil {
		return workflow.AuthoringPage{}, ErrDecisionAuthorizationUnavailable
	}
	if actor.UserID != s.session.ActorID.String() || actor.SessionID != s.session.SessionID.String() || actor.Version != s.session.MembershipVersion {
		return workflow.AuthoringPage{}, workflow.ErrAuthoringDenied
	}
	c := s.configuration
	result := workflow.AuthoringPage{Rules: []workflow.CaseRule{}}
	err := mysqlstore.Snapshot(ctx, c.db, func(tx *sql.Tx) error {
		r := authoringRepository{platformRepository: platformRepository{tx, c.schema, c.tenant, c.maxRows, c.maxBytes, false}}
		args, err := r.ids(tenant, scenario)
		if err != nil {
			return err
		}
		if err := s.authorize(ctx, tx, c.tenant, AuthoringWorkflowRead); err != nil {
			return err
		}
		var name mysqlstore.Text
		var size int64
		if err := tx.QueryRowContext(ctx, "SELECT "+r.projection("name")+" FROM "+r.schema+".decision_engine_scenarios WHERE tenant_id=? AND id=?", args...).Scan(&size, &name); err != nil {
			return err
		}
		if size < 0 || size > int64(c.maxBytes) {
			return ErrProjectionLimit
		}
		result.ScenarioName = string(name)
		var legacy mysqlstore.Bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+r.schema+".decision_engine_workflows WHERE tenant_id=? AND scenario_id=? AND active=1)", args...).Scan(&legacy); err != nil {
			return err
		}
		result.LegacyWorkflows = bool(legacy)
		values, err := (WorkflowRuleRepository{r}).ListStructured(ctx, tenant, scenario)
		if err != nil {
			return err
		}
		decoded := make([]workflow.CaseRule, 0, len(values))
		seen := map[uuid.UUID]bool{}
		ids := []uuid.UUID{}
		for _, v := range values {
			item, ok := workflow.DecodeCaseRule(v)
			if !ok {
				result.OtherRules++
				continue
			}
			decoded = append(decoded, item)
			if item.ActionType != workflow.ActionTypeDoNothing {
				id, err := uuid.Parse(item.InboxID)
				if err != nil {
					return err
				}
				if !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
		}
		visible, err := s.inboxes.Visible(ctx, tx, c.tenant, ids)
		if err != nil {
			return err
		}
		used := len(result.ScenarioName)
		for _, item := range decoded {
			if item.ActionType != workflow.ActionTypeDoNothing && !visible[uuid.MustParse(item.InboxID)] {
				continue
			}
			raw, err := json.Marshal(item)
			if err != nil {
				return err
			}
			used += len(raw)
			if used > c.maxBytes {
				return ErrProjectionLimit
			}
			result.Rules = append(result.Rules, item)
		}
		return nil
	})
	if err != nil {
		return workflow.AuthoringPage{}, browserAuthoringError(err)
	}
	return result, nil
}

func (s *BrowserWorkflowStore) mutate(ctx context.Context, tenant, scenario string, actor workflow.AuthoringActor, fn func(authoringRepository) error) error {
	if s == nil || s.configuration == nil || s.authorize == nil || fn == nil {
		return ErrDecisionAuthorizationUnavailable
	}
	// These are trusted gateway bindings, not permission claims from the body.
	if actor.UserID != s.session.ActorID.String() || actor.SessionID != s.session.SessionID.String() || actor.Version != s.session.MembershipVersion {
		return workflow.ErrAuthoringDenied
	}
	c := s.configuration
	err := mysqlstore.Mutation(ctx, c.db, func(tx *sql.Tx) error {
		r := authoringRepository{platformRepository: platformRepository{tx, c.schema, c.tenant, c.maxRows, c.maxBytes, true}, workflowGuards: make(map[uuid.UUID]bool)}
		if _, err := r.ids(tenant, scenario); err != nil {
			return err
		}
		if err := s.authorize(ctx, tx, c.tenant, AuthoringWorkflowWrite); err != nil {
			return err
		}
		return fn(r)
	})
	return browserAuthoringError(err)
}

// load uses the tenant/scenario/id index, not a scan of all workflow graphs.
// The common authoring guard precedes this row lock. Child writers take the same
// guard then parent lock, so the parent/children cannot change during assembly.
func browserWorkflowLoad(ctx context.Context, r authoringRepository, tenant, scenario, id string) (workflow.StructuredRule, bool, error) {
	return browserWorkflowRead(ctx, r, tenant, scenario, id, true)
}

// An unlocked preview is only permission-preparation input. Mutations must
// reload under the authoring guard and fence its version before using it.
func browserWorkflowRead(ctx context.Context, r authoringRepository, tenant, scenario, id string, lock bool) (workflow.StructuredRule, bool, error) {
	args, err := r.ids(tenant, scenario, id)
	if err != nil {
		return workflow.StructuredRule{}, false, err
	}
	repo := WorkflowRuleRepository{r}
	var deleted mysqlstore.NullDateTime
	var found mysqlstore.UUID
	query := "SELECT id,deleted_at FROM " + r.schema + ".decision_engine_workflow_rules WHERE tenant_id=? AND scenario_id=? AND id=?"
	if lock {
		query += " FOR UPDATE"
	}
	if err := r.tx.QueryRowContext(ctx, query, args...).Scan(&found, &deleted); err != nil {
		return workflow.StructuredRule{}, false, err
	}
	parent, err := platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, repo.selectSQL()+" WHERE tenant_id=? AND scenario_id=? AND id=?", args...), repo.scan)
	if err != nil {
		return workflow.StructuredRule{}, false, err
	}
	conditions, err := (WorkflowConditionRepository{workflowChildRepository{r, false}}).ListByRule(ctx, tenant, id)
	if err != nil {
		return workflow.StructuredRule{}, false, err
	}
	actions, err := (WorkflowActionRepository{workflowChildRepository{r, true}}).ListByRule(ctx, tenant, id)
	if err != nil {
		return workflow.StructuredRule{}, false, err
	}
	used := len(parent.Name)
	for _, c := range conditions {
		used += len(c.Function) + len(c.Params)
	}
	for _, a := range actions {
		used += len(a.ActionType) + len(a.ActionConfig)
	}
	if used > r.maxBytes {
		return workflow.StructuredRule{}, false, ErrProjectionLimit
	}
	return workflow.StructuredRule{Rule: parent, Conditions: conditions, Actions: actions}, deleted.Valid, nil
}

func browserNoLegacy(ctx context.Context, r authoringRepository, tenant, scenario string) error {
	args, err := r.ids(tenant, scenario)
	if err != nil {
		return err
	}
	var legacy mysqlstore.Bool
	if err := r.tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+r.schema+".decision_engine_workflows WHERE tenant_id=? AND scenario_id=? AND active=1)", args...).Scan(&legacy); err != nil {
		return err
	}
	if legacy {
		return workflow.ErrAuthoringConflict
	}
	return nil
}

func (s *BrowserWorkflowStore) inbox(ctx context.Context, r authoringRepository, rule workflow.CaseRule, active bool) error {
	if rule.ActionType == workflow.ActionTypeDoNothing {
		return nil
	}
	id, err := uuid.Parse(rule.InboxID)
	if err != nil || id == uuid.Nil {
		return ErrTenantScope
	}
	return s.inboxes.Authorize(ctx, r.tx, r.tenant, id, true, active)
}

func browserAudit(ctx context.Context, r authoringRepository, scenario string, actor uuid.UUID, rule workflow.CaseRule, deleting bool, at time.Time) error {
	var value any = rule
	if deleting {
		value = struct {
			Operation string            `json:"operation"`
			Rule      workflow.CaseRule `json:"rule"`
		}{"delete", rule}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(raw) > r.maxBytes {
		return ErrProjectionLimit
	}
	args, err := r.ids(r.tenant.String(), scenario, rule.ID)
	if err != nil {
		return err
	}
	_, err = r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_workflow_authoring_events(id,tenant_id,scenario_id,rule_id,actor_id,snapshot,created_at) VALUES(?,?,?,?,?,?,?)", mysqlstore.UUID(uuid.New()), args[0], args[1], args[2], mysqlstore.UUID(actor), mysqlstore.JSON(raw), mysqlstore.DateTime{Time: at})
	return err
}

func (s *BrowserWorkflowStore) Save(ctx context.Context, tenant, scenario string, actor workflow.AuthoringActor, in workflow.CaseRule) (workflow.CaseRule, error) {
	if err := in.Validate(); err != nil {
		return workflow.CaseRule{}, err
	}
	if in.Condition != nil {
		in.Outcomes = []string{}
	}
	if in.TagIDs == nil {
		in.TagIDs = []string{}
	}
	var result workflow.CaseRule
	err := s.mutate(ctx, tenant, scenario, actor, func(r authoringRepository) error {
		// All input is validated and byte-bounded before writes. Params/config are
		// persisted as JSON with errors propagated, never ignored marshal results.
		params, err := json.Marshal(in.Outcomes)
		version := 1
		if in.Condition != nil {
			version = 2
			params, err = json.Marshal(in.Condition)
		}
		if err != nil {
			return err
		}
		cfg, err := json.Marshal(map[string]any{"authoring_version": version, "inbox_id": in.InboxID, "name": in.Title, "title_mode": in.TitleMode, "tag_ids": in.TagIDs})
		if err != nil {
			return err
		}
		if err := r.scope(tenant, in.Name, string(params), string(cfg)); err != nil {
			return err
		}
		preview, _, previewErr := browserWorkflowRead(ctx, r, tenant, scenario, in.ID, false)
		if previewErr != nil && !errors.Is(previewErr, sql.ErrNoRows) {
			return previewErr
		}
		inboxIDs := map[uuid.UUID]bool{}
		if in.ActionType != workflow.ActionTypeDoNothing {
			inboxIDs[uuid.MustParse(in.InboxID)] = true
		}
		if previewErr == nil {
			old, ok := workflow.DecodeCaseRule(preview)
			if !ok {
				return workflow.ErrAuthoringConflict
			}
			if old.ActionType != workflow.ActionTypeDoNothing {
				inboxIDs[uuid.MustParse(old.InboxID)] = true
			}
		}
		// Check both old/new inboxes in deterministic order BEFORE any owner
		// locks. Historical inboxes may be inactive; new routing must be active
		// when the rule is enabled. The second check only touches already-held
		// inbox authority, so it cannot invert inbox lock ordering.
		if err := s.inboxes.AuthorizeBatch(ctx, r.tx, r.tenant, sortedWorkflowIDs(inboxIDs), true, false); err != nil {
			return err
		}
		if err := s.inbox(ctx, r, in, in.Enabled); err != nil {
			return err
		}
		tags := make([]uuid.UUID, len(in.TagIDs))
		for i, id := range in.TagIDs {
			tags[i], err = uuid.Parse(id)
			if err != nil {
				return err
			}
		}
		if err := s.tags.Authorize(ctx, r.tx, r.tenant, tags, true, in.Enabled); err != nil {
			return err
		}
		if err := r.lockWorkflowAuthoring(ctx, tenant, scenario); err != nil {
			return err
		}
		if err := browserNoLegacy(ctx, r, tenant, scenario); err != nil {
			return err
		}
		current, deleted, err := browserWorkflowLoad(ctx, r, tenant, scenario, in.ID)
		creating := errors.Is(err, sql.ErrNoRows)
		if err != nil && !creating {
			return err
		}
		if creating != errors.Is(previewErr, sql.ErrNoRows) || !creating && !current.Rule.UpdatedAt.Equal(preview.Rule.UpdatedAt) {
			return workflow.ErrAuthoringConflict
		}
		if deleted {
			return workflow.ErrAuthoringConflict
		}
		priority := 0
		if creating {
			if in.UpdatedAt != nil {
				return workflow.ErrAuthoringConflict
			}
			args, _ := r.ids(tenant, scenario)
			var count int
			var maximum sql.NullInt64
			if err := r.tx.QueryRowContext(ctx, "SELECT COUNT(*),MAX(priority) FROM (SELECT priority FROM "+r.schema+".decision_engine_workflow_rules WHERE tenant_id=? AND scenario_id=? AND deleted_at IS NULL LIMIT 501) AS bounded_rules", args...).Scan(&count, &maximum); err != nil {
				return err
			}
			if count >= min(r.maxRows, 500) || maximum.Valid && maximum.Int64 >= math.MaxInt32 {
				return workflow.ErrAuthoringConflict
			}
			if maximum.Valid && maximum.Int64 >= 0 {
				priority = int(maximum.Int64) + 1
			}
		} else {
			old, ok := workflow.DecodeCaseRule(current)
			if !ok {
				return workflow.ErrAuthoringConflict
			}
			oldVersion := old.UpdatedAt
			old.UpdatedAt = nil
			check := in
			check.UpdatedAt = nil
			if reflect.DeepEqual(old, check) {
				old.UpdatedAt = oldVersion
				result = old
				return nil
			}
			if in.UpdatedAt == nil || !in.UpdatedAt.Equal(*oldVersion) {
				return workflow.ErrAuthoringConflict
			}
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		if !creating && !now.After(current.Rule.UpdatedAt) {
			now = current.Rule.UpdatedAt.Add(time.Microsecond)
		}
		function := workflow.ConditionNever
		if in.Enabled {
			function = workflow.ConditionOutcomeIn
			if in.Condition != nil {
				function = workflow.ConditionTreeMatch
			}
		}
		rules := WorkflowRuleRepository{r}
		conditions := WorkflowConditionRepository{workflowChildRepository{r, false}}
		actions := WorkflowActionRepository{workflowChildRepository{r, true}}
		if creating {
			if _, err := rules.Create(ctx, workflow.Rule{ID: in.ID, TenantID: tenant, ScenarioID: scenario, Name: in.Name, Priority: priority, CreatedAt: now, UpdatedAt: now}); err != nil {
				return err
			}
			if _, err := conditions.Create(ctx, workflow.Condition{ID: uuid.NewString(), TenantID: tenant, RuleID: in.ID, Function: function, Params: params, CreatedAt: now, UpdatedAt: now}); err != nil {
				return err
			}
			if _, err := actions.Create(ctx, workflow.Action{ID: uuid.NewString(), TenantID: tenant, RuleID: in.ID, ActionType: in.ActionType, ActionConfig: cfg, CreatedAt: now, UpdatedAt: now}); err != nil {
				return err
			}
		} else {
			rule := current.Rule
			rule.Name = in.Name
			rule.UpdatedAt = now
			if _, err := rules.Update(ctx, rule); err != nil {
				return err
			}
			c := current.Conditions[0]
			c.Function = function
			c.Params = params
			c.UpdatedAt = now
			if _, err := conditions.Update(ctx, c); err != nil {
				return err
			}
			a := current.Actions[0]
			a.ActionType = in.ActionType
			a.ActionConfig = cfg
			a.UpdatedAt = now
			if _, err := actions.Update(ctx, a); err != nil {
				return err
			}
		}
		in.UpdatedAt = &now
		if err := browserAudit(ctx, r, scenario, s.session.ActorID, in, false, now); err != nil {
			return err
		}
		result = in
		return nil
	})
	if err != nil {
		return workflow.CaseRule{}, err
	}
	return result, nil
}

// Delete preserves child identities and the optimistic version on the tombstone.
// A replay with that version is idempotent, but still reauthorizes the actor.
func (s *BrowserWorkflowStore) Delete(ctx context.Context, tenant, scenario string, actor workflow.AuthoringActor, id string, expected time.Time) error {
	return s.mutate(ctx, tenant, scenario, actor, func(r authoringRepository) error {
		preview, _, err := browserWorkflowRead(ctx, r, tenant, scenario, id, false)
		if err != nil {
			return err
		}
		old, ok := workflow.DecodeCaseRule(preview)
		if !ok {
			return workflow.ErrAuthoringConflict
		}
		if err := s.inbox(ctx, r, old, false); err != nil {
			return err
		}
		if err := r.lockWorkflowAuthoring(ctx, tenant, scenario); err != nil {
			return err
		}
		current, deleted, err := browserWorkflowLoad(ctx, r, tenant, scenario, id)
		if err != nil {
			return err
		}
		if !current.Rule.UpdatedAt.Equal(preview.Rule.UpdatedAt) {
			return workflow.ErrAuthoringConflict
		}
		if !expected.Equal(current.Rule.UpdatedAt) {
			return workflow.ErrAuthoringConflict
		}
		if deleted {
			return nil
		}
		if err := browserNoLegacy(ctx, r, tenant, scenario); err != nil {
			return err
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		// Update the condition before tombstoning: ordinary child mutations reject
		// deleted parents. Historical action rows remain usable by pending jobs.
		c := current.Conditions[0]
		c.Function = workflow.ConditionNever
		c.UpdatedAt = now
		if _, err := (WorkflowConditionRepository{workflowChildRepository{r, false}}).Update(ctx, c); err != nil {
			return err
		}
		if err := browserAudit(ctx, r, scenario, s.session.ActorID, old, true, now); err != nil {
			return err
		}
		return (WorkflowRuleRepository{r}).Delete(ctx, tenant, scenario, id)
	})
}
