package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// Copy preserves browser rule meaning with new graph identities and appends to
// the destination's priority order. A mandatory preparation guard checks copied
// conditions against the actual destination model, locally on the transaction.
// Unsupported/hidden graphs are rejected, never silently omitted. This staged
// consumer remains inactive until real auth/case providers are supplied.
func (s *BrowserWorkflowStore) Copy(ctx context.Context, tenant, source, destination string, actor workflow.AuthoringActor, dependencies WorkflowCopyGuard) ([]workflow.CaseRule, error) {
	if s == nil || s.configuration == nil || s.authorize == nil || dependencies == nil {
		return nil, ErrDecisionAuthorizationUnavailable
	}
	if actor.UserID != s.session.ActorID.String() || actor.SessionID != s.session.SessionID.String() || actor.Version != s.session.MembershipVersion {
		return nil, workflow.ErrAuthoringDenied
	}
	c := s.configuration
	t, err := uuid.Parse(tenant)
	if err != nil || t != c.tenant {
		return nil, ErrTenantScope
	}
	src, err := uuid.Parse(source)
	if err != nil || src == uuid.Nil {
		return nil, ErrTenantScope
	}
	dst, err := uuid.Parse(destination)
	if err != nil || dst == uuid.Nil || src == dst {
		return nil, ErrTenantScope
	}
	config, err := NewConfigurationStore(c.db, strings.Trim(c.schema, "`"), c.tenant, c.maxRows, c.maxBytes, s.authorize)
	if err != nil {
		return nil, err
	}
	guard := func(ctx context.Context, tx *sql.Tx, snapshot ScenarioCopySnapshot, target scenario.Scenario) error {
		inboxes, tags := map[uuid.UUID]bool{}, map[uuid.UUID]bool{}
		for _, graph := range snapshot.Workflows {
			rule, ok := workflow.DecodeCaseRule(graph)
			if !ok {
				return workflow.ErrAuthoringConflict
			}
			if err := rule.Validate(); err != nil {
				return err
			}
			if rule.ActionType != workflow.ActionTypeDoNothing {
				inboxes[uuid.MustParse(rule.InboxID)] = true
			}
			for _, id := range rule.TagIDs {
				tags[uuid.MustParse(id)] = true
			}
		}
		if err := s.inboxes.AuthorizeBatch(ctx, tx, t, sortedWorkflowIDs(inboxes), true, true); err != nil {
			return err
		}
		// Bounded batches, not a tag/permission query for every workflow rule.
		ordered := sortedWorkflowIDs(tags)
		if len(ordered) == 0 {
			if err := s.tags.Authorize(ctx, tx, t, nil, true, true); err != nil {
				return err
			}
		}
		for offset := 0; offset < len(ordered); offset += 25 {
			if err := s.tags.Authorize(ctx, tx, t, ordered[offset:min(offset+25, len(ordered))], true, true); err != nil {
				return err
			}
		}
		return dependencies(ctx, tx, snapshot, target)
	}
	before := func(ctx context.Context, r authoringRepository, copied []workflow.StructuredRule) error {
		// Match all other structured writers' per-scenario guard. Permission
		// providers have already run; no provider call is made under this lock.
		if err := r.lockWorkflowAuthoring(ctx, tenant, destination); err != nil {
			return err
		}
		if err := browserNoLegacy(ctx, r, tenant, destination); err != nil {
			return err
		}
		var count int
		var maximum sql.NullInt64
		if err := r.tx.QueryRowContext(ctx, "SELECT COUNT(*),MAX(priority) FROM (SELECT priority FROM "+r.schema+".decision_engine_workflow_rules WHERE tenant_id=? AND scenario_id=? AND deleted_at IS NULL LIMIT 501) bounded_rules", mysqlstore.UUID(t), mysqlstore.UUID(dst)).Scan(&count, &maximum); err != nil {
			return err
		}
		if count+len(copied) > min(r.maxRows, 500) || maximum.Valid && maximum.Int64 > math.MaxInt32-int64(len(copied)) {
			return workflow.ErrAuthoringConflict
		}
		base := int64(0)
		if maximum.Valid && maximum.Int64 >= 0 {
			base = maximum.Int64 + 1
		}
		for i := range copied {
			copied[i].Rule.Priority = int(base) + i
		}
		return nil
	}
	var result []workflow.CaseRule
	after := func(ctx context.Context, r authoringRepository, copied []workflow.StructuredRule) error {
		result = make([]workflow.CaseRule, len(copied))
		for i, graph := range copied {
			rule, ok := workflow.DecodeCaseRule(graph)
			if !ok {
				return workflow.ErrAuthoringConflict
			}
			result[i] = rule
		}
		return browserCopyAudit(ctx, r, src, dst, s.session.ActorID, copied, result)
	}
	_, err = config.copyWorkflowRules(ctx, src, dst, guard, before, after)
	if err != nil {
		return nil, browserAuthoringError(err)
	}
	return result, nil
}

func sortedWorkflowIDs(values map[uuid.UUID]bool) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	return ids
}

func browserCopyAudit(ctx context.Context, r authoringRepository, source, destination, actor uuid.UUID, graphs []workflow.StructuredRule, rules []workflow.CaseRule) error {
	if len(graphs) != len(rules) || actor == uuid.Nil {
		return errors.New("copy audit requires trusted actor and complete graph")
	}
	if len(rules) == 0 {
		return nil // No mutation, therefore no fabricated rule audit identity.
	}
	args := make([]any, 0, len(rules)*7)
	values := make([]string, len(rules))
	used := 0
	for i, rule := range rules {
		raw, err := json.Marshal(struct {
			Operation string            `json:"operation"`
			SourceID  string            `json:"source_scenario_id"`
			Rule      workflow.CaseRule `json:"rule"`
		}{"copy", source.String(), rule})
		if err != nil {
			return err
		}
		used += len(raw)
		if used > r.maxBytes {
			return ErrProjectionLimit
		}
		values[i] = "(?,?,?,?,?,?,?)"
		args = append(args, mysqlstore.UUID(uuid.New()), mysqlstore.UUID(r.tenant), mysqlstore.UUID(destination), mysqlstore.UUID(uuid.MustParse(rule.ID)), mysqlstore.UUID(actor), mysqlstore.JSON(raw), mysqlstore.DateTime{Time: graphs[i].Rule.UpdatedAt})
	}
	_, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_workflow_authoring_events(id,tenant_id,scenario_id,rule_id,actor_id,snapshot,created_at) VALUES"+strings.Join(values, ","), args...)
	return err
}
