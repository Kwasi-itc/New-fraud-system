package mysql

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func allowAuthoringFixture(context.Context, *sql.Tx, uuid.UUID, AuthoringAction) error { return nil }
func authoringStore(t *testing.T, f platformFixture, tenant uuid.UUID, rows, bytes int) *AuthoringStore {
	t.Helper()
	s, err := NewAuthoringStore(f.db, f.database, tenant, rows, bytes, allowAuthoringFixture)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func authoringDraft(tenant, parent uuid.UUID) scenario.Iteration {
	a, b, c := 1, 10, 20
	return scenario.Iteration{ID: uuid.NewString(), TenantID: tenant.String(), ScenarioID: parent.String(), Status: scenario.IterationStatusDraft, TriggerFormula: json.RawMessage(`{"constant":true}`), ScoreReviewThreshold: &a, ScoreBlockAndReviewThreshold: &b, ScoreDeclineThreshold: &c, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
}
func authoringRule(tenant uuid.UUID, iteration string, order int) scenario.Rule {
	at := time.Now().UTC().Truncate(time.Microsecond)
	return scenario.Rule{ID: uuid.NewString(), TenantID: tenant.String(), IterationID: iteration, DisplayOrder: order, Name: fmt.Sprintf("rule %d", order), Formula: json.RawMessage(`{"constant":true}`), ScoreModifier: 35, StableRuleID: fmt.Sprintf("stable %d", order), RuleGroup: "Funding", CreatedAt: at, UpdatedAt: at}
}
func createAuthoringDraft(t *testing.T, s *AuthoringStore, tenant, parent uuid.UUID) scenario.Iteration {
	t.Helper()
	var out scenario.Iteration
	err := s.Mutate(context.Background(), func(r AuthoringRepositories) error {
		v := authoringDraft(tenant, parent)
		version, err := r.Iterations.NextVersion(context.Background(), v.TenantID, v.ScenarioID)
		if err != nil {
			return err
		}
		v.Version = version
		out, err = r.Iterations.Create(context.Background(), v)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestMySQLAuthoringCRUDOrderingBoundsAndIsolation(t *testing.T) {
	f := scenarioFoundationDB(t, true)
	ctx := context.Background()
	tenant, parent := uuid.New(), uuid.New()
	s := authoringStore(t, f, tenant, 500, 2<<20)
	err := s.Mutate(ctx, func(r AuthoringRepositories) error {
		at := time.Now().UTC().Truncate(time.Microsecond)
		_, err := r.Scenarios.Create(ctx, scenario.Scenario{ID: parent.String(), TenantID: tenant.String(), Name: "Owner scenario", Description: "test", TriggerObjectType: "wallets", CreatedAt: at, UpdatedAt: at})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	draft := createAuthoringDraft(t, s, tenant, parent)
	items := make([]scenario.Rule, 120)
	for i := range items {
		items[i] = authoringRule(tenant, draft.ID, len(items)-i)
	}
	err = s.Mutate(ctx, func(r AuthoringRepositories) error {
		created, err := r.Rules.CreateMany(ctx, tenant.String(), parent.String(), draft.ID, items)
		if err != nil {
			return err
		}
		if len(created) != len(items) || created[0].ID != items[0].ID {
			t.Fatal("batch result order")
		}
		listed, err := r.Rules.ListByIteration(ctx, tenant.String(), parent.String(), draft.ID)
		if err != nil {
			return err
		}
		if len(listed) != 120 || listed[0].DisplayOrder != 1 || listed[119].DisplayOrder != 120 {
			t.Fatal("rule display order")
		}
		listed[0].ScoreModifier = 45
		listed[0].Description = "updated"
		if _, err := r.Rules.Update(ctx, listed[0]); err != nil {
			return err
		}
		if err := r.Rules.Delete(ctx, tenant.String(), parent.String(), draft.ID, listed[1].ID); err != nil {
			return err
		}
		groups, err := r.Rules.ListRuleGroupsByScenario(ctx, tenant.String(), parent.String())
		if err != nil {
			return err
		}
		if len(groups) != 1 || groups[0] != "Funding" {
			t.Fatal(groups)
		}
		current, err := r.Scenarios.GetByID(ctx, tenant.String(), parent.String())
		if err != nil {
			return err
		}
		if _, err := r.Scenarios.Update(ctx, current); err != nil {
			return err
		}
		if err := r.Scenarios.SetLiveIterationID(ctx, tenant.String(), parent.String(), &draft.ID); !errors.Is(err, ErrPublicationOperationRequired) {
			t.Fatal("unguarded publication", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = s.ReadSnapshot(ctx, func(r AuthoringRepositories) error {
		if _, err := r.Scenarios.GetByID(ctx, uuid.NewString(), parent.String()); !errors.Is(err, ErrTenantScope) {
			t.Fatal("tenant leakage", err)
		}
		if _, err := r.Scenarios.ListByTenant(ctx, tenant.String()); err != nil {
			return err
		}
		if _, err := r.Iterations.ListLiveScheduled(ctx, 1); !errors.Is(err, ErrSystemScopeRequired) {
			t.Fatal("global worker operation escaped tenant scope", err)
		}
		return r.Rules.Delete(ctx, tenant.String(), parent.String(), draft.ID, items[0].ID)
	})
	if !errors.Is(err, ErrAuthoringReadOnly) {
		t.Fatal("snapshot mutation", err)
	}
	small := authoringStore(t, f, tenant, 1, 2<<20)
	if err := small.ReadSnapshot(ctx, func(r AuthoringRepositories) error {
		_, err := r.Rules.ListByIteration(ctx, tenant.String(), parent.String(), draft.ID)
		return err
	}); !errors.Is(err, ErrProjectionLimit) {
		t.Fatal("unbounded rules", err)
	}
	tiny := authoringStore(t, f, tenant, 500, 4)
	if err := tiny.ReadSnapshot(ctx, func(r AuthoringRepositories) error {
		_, err := r.Scenarios.GetByID(ctx, tenant.String(), parent.String())
		return err
	}); !errors.Is(err, ErrProjectionLimit) {
		t.Fatal("unbounded projection", err)
	}
}

func TestMySQLAuthoringCommitCloneAndPublicationAtomicity(t *testing.T) {
	f := scenarioFoundationDB(t, true)
	ctx := context.Background()
	tenant, parent := uuid.New(), uuid.New()
	s := authoringStore(t, f, tenant, 100, 1<<20)
	schema, _ := mysqlstore.Identifier(f.database)
	if err := scenarioInsert(ctx, f.db, f.database, tenant, parent, "publish"); err != nil {
		t.Fatal(err)
	}
	first := createAuthoringDraft(t, s, tenant, parent)
	if err := s.Mutate(ctx, func(r AuthoringRepositories) error {
		if _, err := r.Rules.Create(ctx, authoringRule(tenant, first.ID, 1)); err != nil {
			return err
		}
		var err error
		first, err = r.Iterations.Commit(ctx, tenant.String(), parent.String(), first.ID, time.Now().UTC().Truncate(time.Microsecond))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Mutate(ctx, func(r AuthoringRepositories) error {
		_, err := r.Rules.Create(ctx, authoringRule(tenant, first.ID, 2))
		return err
	}); !errors.Is(err, ErrCommittedIteration) {
		t.Fatal("committed rule edit", err)
	}
	if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".decision_engine_scenario_iterations SET schedule='changed' WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(uuid.MustParse(first.ID))); err == nil {
		t.Fatal("SQL bypass of committed immutability")
	}
	clone, err := s.CloneIteration(ctx, parent, uuid.MustParse(first.ID), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if clone.Version != 2 || clone.Status != scenario.IterationStatusDraft || clone.CommittedAt != nil {
		t.Fatal("clone state", clone)
	}
	if err := s.ReadSnapshot(ctx, func(r AuthoringRepositories) error {
		rules, err := r.Rules.ListByIteration(ctx, tenant.String(), parent.String(), clone.ID)
		if err != nil {
			return err
		}
		if len(rules) != 1 || rules[0].StableRuleID != "stable 1" || rules[0].ScoreModifier != 35 {
			t.Fatal("clone rule fidelity", rules)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	guardCalls := 0
	guard := func(_ context.Context, tx *sql.Tx, snapshot PublicationSnapshot) error {
		if tx == nil || snapshot.Scenario.TenantID != tenant.String() || len(snapshot.Rules) != 1 {
			t.Fatal("guard binding")
		}
		guardCalls++
		return nil
	}
	input := PublicationInput{parent, uuid.MustParse(first.ID), uuid.New(), uuid.New()}
	if _, err := s.Publish(ctx, input, nil); err == nil {
		t.Fatal("missing preparation guard accepted")
	}
	if _, err := s.Publish(ctx, input, guard); err != nil {
		t.Fatal(err)
	}
	if err := s.Mutate(ctx, func(r AuthoringRepositories) error {
		_, err := r.Iterations.Commit(ctx, tenant.String(), parent.String(), clone.ID, time.Now().UTC().Truncate(time.Microsecond))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// Fail the new publication after the old-unpublish event and link replacement.
	if _, err := f.db.ExecContext(ctx, "CREATE TRIGGER "+schema+".fixture_fail_publish BEFORE INSERT ON "+schema+".decision_engine_scenario_publications FOR EACH ROW BEGIN IF NEW.action='publish' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fixture'; END IF; END"); err != nil {
		t.Fatal(err)
	}
	replacement := PublicationInput{parent, uuid.MustParse(clone.ID), uuid.New(), uuid.New()}
	if _, err := s.Publish(ctx, replacement, guard); err == nil {
		t.Fatal("publication failure hidden")
	}
	if err := s.ReadSnapshot(ctx, func(r AuthoringRepositories) error {
		current, err := r.Scenarios.GetByID(ctx, tenant.String(), parent.String())
		if err != nil {
			return err
		}
		if current.LiveIterationID == nil || *current.LiveIterationID != first.ID {
			t.Fatal("partial live replacement")
		}
		events, err := r.Publications.ListByScenario(ctx, tenant.String(), parent.String())
		if err != nil {
			return err
		}
		if len(events) != 1 {
			t.Fatal("partial publication audit", events)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(ctx, "DROP TRIGGER "+schema+".fixture_fail_publish"); err != nil {
		t.Fatal(err)
	}
	events, err := s.Publish(ctx, replacement, guard)
	if err != nil || len(events) != 2 {
		t.Fatal("replacement", events, err)
	}
	if events[0].Action != scenario.PublicationActionUnpublish || events[1].Action != scenario.PublicationActionPublish {
		t.Fatal("publication event order")
	}
	if _, err := s.Unpublish(ctx, PublicationInput{ScenarioID: parent, IterationID: uuid.MustParse(clone.ID), EventID: uuid.New()}); err != nil {
		t.Fatal(err)
	}
	if guardCalls != 3 {
		t.Fatal("guard calls", guardCalls)
	}
	denied, err := NewAuthoringStore(f.db, f.database, tenant, 100, 1<<20, func(context.Context, *sql.Tx, uuid.UUID, AuthoringAction) error {
		return ErrDecisionAuthorizationDenied
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := denied.Publish(ctx, input, guard); !errors.Is(err, ErrDecisionAuthorizationDenied) {
		t.Fatal("publish authorization bypass", err)
	}
}

func TestMySQLAuthoringCommitWaitsForRuleMutation(t *testing.T) {
	f := scenarioFoundationDB(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tenant, parent := uuid.New(), uuid.New()
	s := authoringStore(t, f, tenant, 100, 1<<20)
	if err := scenarioInsert(ctx, f.db, f.database, tenant, parent, "commit race"); err != nil {
		t.Fatal(err)
	}
	draft := createAuthoringDraft(t, s, tenant, parent)
	inserted, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	edited := make(chan error, 1)
	committed := make(chan error, 1)
	go func() {
		edited <- s.Mutate(ctx, func(r AuthoringRepositories) error {
			if _, err := r.Rules.Create(ctx, authoringRule(tenant, draft.ID, 1)); err != nil {
				return err
			}
			close(inserted)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-inserted:
	case err := <-edited:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	go func() {
		committed <- s.Mutate(ctx, func(r AuthoringRepositories) error {
			_, err := r.Iterations.Commit(ctx, tenant.String(), parent.String(), draft.ID, time.Now().UTC().Truncate(time.Microsecond))
			return err
		})
	}()
	deadline := time.Now().Add(3 * time.Second)
	waiting := false
	for time.Now().Before(deadline) {
		var count int
		if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM performance_schema.data_lock_waits WHERE REQUESTING_ENGINE_TRANSACTION_ID IS NOT NULL").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count > 0 {
			waiting = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("commit did not wait for the rule guard")
	}
	once.Do(func() { close(release) })
	if err := <-edited; err != nil {
		t.Fatal(err)
	}
	if err := <-committed; err != nil {
		t.Fatal(err)
	}
	if err := s.Mutate(ctx, func(r AuthoringRepositories) error {
		_, err := r.Rules.Create(ctx, authoringRule(tenant, draft.ID, 2))
		return err
	}); !errors.Is(err, ErrCommittedIteration) {
		t.Fatal("post-commit mutation", err)
	}
}

func TestMySQLDecisionAuthConsumerContractFailsClosed(t *testing.T) {
	f := scenarioFoundationDB(t, true)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.database)
	session := TrustedDecisionSession{ActorID: uuid.New(), TenantID: uuid.New(), SessionID: uuid.New(), MembershipVersion: 9}
	authorize, err := SessionAuthorizer(f.database, session)
	if err != nil {
		t.Fatal(err)
	}
	check := func(action AuthoringAction) error {
		return mysqlstore.Mutation(ctx, f.db, func(tx *sql.Tx) error { return authorize(ctx, tx, session.TenantID, action) })
	}
	if err := check(AuthoringRead); !errors.Is(err, ErrDecisionAuthorizationUnavailable) {
		t.Fatal("missing auth provider allowed", err)
	}
	// Contract fixture only, not the AU-01 provider or a production role policy.
	hexID := func(id uuid.UUID) string { return hex.EncodeToString(id[:]) }
	stmt := fmt.Sprintf("CREATE FUNCTION %s.identity_authorize_decision_v1(a BINARY(16),t BINARY(16),s BINARY(16),v BIGINT,act VARCHAR(32),locks BOOLEAN) RETURNS BOOLEAN DETERMINISTIC NO SQL RETURN a=UNHEX('%s') AND t=UNHEX('%s') AND s=UNHEX('%s') AND v=9 AND act IN ('decision.read','decision.author') AND locks=(act<>'decision.read')", schema, hexID(session.ActorID), hexID(session.TenantID), hexID(session.SessionID))
	if _, err := f.db.ExecContext(ctx, stmt); err != nil {
		t.Fatal(err)
	}
	if err := check(AuthoringRead); err != nil {
		t.Fatal(err)
	}
	if err := check(AuthoringWrite); err != nil {
		t.Fatal(err)
	}
	if err := check(AuthoringPublish); !errors.Is(err, ErrDecisionAuthorizationDenied) {
		t.Fatal("publish action collapsed into write", err)
	}
	if err := check(AuthoringAction("arbitrary")); !errors.Is(err, ErrDecisionAuthorizationDenied) {
		t.Fatal(err)
	}
	stale := session
	stale.MembershipVersion++
	staleCheck, err := SessionAuthorizer(f.database, stale)
	if err != nil {
		t.Fatal(err)
	}
	if err := mysqlstore.Mutation(ctx, f.db, func(tx *sql.Tx) error { return staleCheck(ctx, tx, session.TenantID, AuthoringRead) }); !errors.Is(err, ErrDecisionAuthorizationDenied) {
		t.Fatal("stale credential", err)
	}
	readOnly := session
	readOnly.ReadOnly = true
	readOnlyCheck, _ := SessionAuthorizer(f.database, readOnly)
	if err := mysqlstore.Mutation(ctx, f.db, func(tx *sql.Tx) error { return readOnlyCheck(ctx, tx, session.TenantID, AuthoringWrite) }); !errors.Is(err, ErrDecisionAuthorizationDenied) {
		t.Fatal("read-only credential", err)
	}
	if _, err := f.db.ExecContext(ctx, "DROP FUNCTION "+schema+".identity_authorize_decision_v1"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(ctx, "CREATE FUNCTION "+schema+".identity_authorize_decision_v1(a BINARY(16),t BINARY(16),s BINARY(16),v BIGINT,act VARCHAR(32),locks BOOLEAN) RETURNS BOOLEAN DETERMINISTIC NO SQL RETURN NULL"); err != nil {
		t.Fatal(err)
	}
	if err := check(AuthoringRead); !errors.Is(err, ErrDecisionAuthorizationUnavailable) {
		t.Fatal("NULL auth result treated as allow/deny", err)
	}
	if _, err := SessionAuthorizer("unsafe`", session); err == nil {
		t.Fatal("unsafe schema")
	}
	bad := session
	bad.ActorID = uuid.Nil
	if _, err := SessionAuthorizer(f.database, bad); err == nil {
		t.Fatal("missing trusted actor")
	}
}

func TestMySQLConcurrentPublicationSerializesOnlyItsScenario(t *testing.T) {
	f := scenarioFoundationDB(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	tenant, parent, other := uuid.New(), uuid.New(), uuid.New()
	s := authoringStore(t, f, tenant, 500, 2<<20)
	for _, id := range []uuid.UUID{parent, other} {
		if err := scenarioInsert(ctx, f.db, f.database, tenant, id, id.String()); err != nil {
			t.Fatal(err)
		}
	}
	iterations := []scenario.Iteration{createAuthoringDraft(t, s, tenant, parent), createAuthoringDraft(t, s, tenant, parent), createAuthoringDraft(t, s, tenant, other)}
	if err := s.Mutate(ctx, func(r AuthoringRepositories) error {
		for _, v := range iterations {
			if _, err := r.Iterations.Commit(ctx, tenant.String(), v.ScenarioID, v.ID, time.Now().UTC().Truncate(time.Microsecond)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	first, second := make(chan error, 1), make(chan error, 1)
	input := func(v scenario.Iteration) PublicationInput {
		return PublicationInput{uuid.MustParse(v.ScenarioID), uuid.MustParse(v.ID), uuid.New(), uuid.New()}
	}
	go func() {
		_, err := s.Publish(ctx, input(iterations[0]), func(context.Context, *sql.Tx, PublicationSnapshot) error {
			close(entered)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		first <- err
	}()
	select {
	case <-entered:
	case err := <-first:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	guard := func(context.Context, *sql.Tx, PublicationSnapshot) error { return nil }
	go func() { _, err := s.Publish(ctx, input(iterations[1]), guard); second <- err }()
	waitCtx, stop := context.WithTimeout(ctx, 3*time.Second)
	defer stop()
	for {
		var waiting bool
		query := "SELECT EXISTS(SELECT 1 FROM performance_schema.data_lock_waits w JOIN performance_schema.data_locks l ON l.ENGINE_LOCK_ID=w.REQUESTING_ENGINE_LOCK_ID AND l.ENGINE=w.ENGINE WHERE l.OBJECT_SCHEMA=? AND l.OBJECT_NAME='decision_engine_scenarios')"
		if err := f.db.QueryRowContext(waitCtx, query, f.database).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case err := <-second:
			t.Fatal("second publication did not wait", err)
		case <-waitCtx.Done():
			t.Fatal(waitCtx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	// Another scenario is not serialized through the blocked parent.
	if _, err := s.Publish(waitCtx, input(iterations[2]), guard); err != nil {
		t.Fatal("unrelated publication blocked", err)
	}
	once.Do(func() { close(release) })
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if err := s.ReadSnapshot(ctx, func(r AuthoringRepositories) error {
		current, err := r.Scenarios.GetByID(ctx, tenant.String(), parent.String())
		if err != nil {
			return err
		}
		if current.LiveIterationID == nil || *current.LiveIterationID != iterations[1].ID {
			t.Fatal("lost serialized live link")
		}
		events, err := r.Publications.ListByScenario(ctx, tenant.String(), parent.String())
		if err != nil {
			return err
		}
		if len(events) != 3 {
			t.Fatal("missing replacement audit", events)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
