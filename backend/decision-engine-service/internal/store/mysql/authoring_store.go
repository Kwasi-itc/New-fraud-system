package mysql

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var ErrCommittedIteration = errors.New("iteration is not draft")
var ErrAuthoringReadOnly = errors.New("authoring mutation requires a write transaction")
var ErrSystemScopeRequired = errors.New("operation requires explicit authorized worker scope")
var ErrPublicationOperationRequired = errors.New("live links and publication events require the guarded publication operation")

type AuthoringAction string

const (
	AuthoringRead    AuthoringAction = "decision.read"
	AuthoringWrite   AuthoringAction = "decision.author"
	AuthoringPublish AuthoringAction = "decision.publish"
	// Browser workflow administration retains the source's platform-admin AND
	// case-access requirement. A generic scenario-author grant is insufficient.
	AuthoringWorkflowRead    AuthoringAction = "decision.workflow.read"
	AuthoringWorkflowWrite   AuthoringAction = "decision.workflow.author"
	AuthoringCustomListRead  AuthoringAction = "decision.list.read"
	AuthoringCustomListWrite AuthoringAction = "decision.list.mutate"
)

type AuthoringAuthorizer func(context.Context, *sql.Tx, uuid.UUID, AuthoringAction) error

// AuthoringStore exposes only implemented authoring repositories, not a partial
// main TransactionManager. Authorization runs before owner locks, on this exact
// transaction. Runtime composition needs the real auth-owned contract.
type AuthoringStore struct {
	configuration *PlatformStore
	authorize     AuthoringAuthorizer
}
type AuthoringRepositories struct {
	Scenarios    ScenarioRepository
	Iterations   ScenarioIterationRepository
	Rules        RuleRepository
	Publications ScenarioPublicationRepository
}

func NewAuthoringStore(db *sql.DB, database string, tenant uuid.UUID, maxRows, maxBytes int, authorize AuthoringAuthorizer) (*AuthoringStore, error) {
	if authorize == nil {
		return nil, errors.New("authoring action authorizer required")
	}
	c, err := NewPlatformStore(db, database, tenant, maxRows, maxBytes, func(ctx context.Context, tx *sql.Tx, tenant uuid.UUID, write bool) error {
		action := AuthoringRead
		if write {
			action = AuthoringWrite
		}
		return authorize(ctx, tx, tenant, action)
	})
	if err != nil {
		return nil, err
	}
	return &AuthoringStore{c, authorize}, nil
}
func (s *AuthoringStore) Mutate(ctx context.Context, fn func(AuthoringRepositories) error) error {
	return s.run(ctx, true, fn)
}
func (s *AuthoringStore) ReadSnapshot(ctx context.Context, fn func(AuthoringRepositories) error) error {
	return s.run(ctx, false, fn)
}
func (s *AuthoringStore) run(ctx context.Context, write bool, fn func(AuthoringRepositories) error) error {
	if s == nil || s.configuration == nil || fn == nil {
		return errors.New("authoring store and callback required")
	}
	c := s.configuration
	run := mysqlstore.Snapshot
	if write {
		run = mysqlstore.Mutation
	}
	return run(ctx, c.db, func(tx *sql.Tx) error {
		if err := c.authorize(ctx, tx, c.tenant, write); err != nil {
			return err
		}
		r := authoringRepository{platformRepository: platformRepository{tx, c.schema, c.tenant, c.maxRows, c.maxBytes, write}}
		return fn(AuthoringRepositories{ScenarioRepository{r}, ScenarioIterationRepository{r}, RuleRepository{r}, ScenarioPublicationRepository{r}})
	})
}

type authoringRepository struct {
	platformRepository
	publication    bool
	workflowGuards map[uuid.UUID]bool
	effectQueues   effectQueueConfig
	effectJobError *error
}

func (r authoringRepository) ids(tenant string, ids ...string) ([]any, error) {
	if err := r.scope(tenant); err != nil {
		return nil, err
	}
	out := []any{mysqlstore.UUID(r.tenant)}
	for _, s := range ids {
		id, err := uuid.Parse(s)
		if err != nil || id == uuid.Nil {
			return nil, ErrTenantScope
		}
		out = append(out, mysqlstore.UUID(id))
	}
	return out, nil
}
func (r authoringRepository) mutating() error {
	if !r.write {
		return ErrAuthoringReadOnly
	}
	return nil
}
func checkedInt(v int) (mysqlstore.Int32, error) {
	if v < math.MinInt32 || v > math.MaxInt32 {
		return 0, errors.New("value outside signed INT range")
	}
	return mysqlstore.Int32(v), nil
}

// A single row cannot materialize unbounded text/JSON before checking budgets.
// JSON is projected as valid JSON null on overflow; the accompanying original
// byte size still causes an explicit error, never a successful no-match.
func (r authoringRepository) project(text []string, jsonColumns ...string) string {
	columns := append(append([]string{}, text...), jsonColumns...)
	lengths := make([]string, len(columns))
	for i, c := range columns {
		lengths[i] = "OCTET_LENGTH(" + c + ")"
	}
	size := strings.Join(lengths, "+")
	parts := []string{size}
	for i, c := range columns {
		absent := "''"
		if i >= len(text) {
			absent = "'null'"
		}
		parts = append(parts, "CASE WHEN "+size+"<="+strconv.Itoa(r.maxBytes)+" THEN "+c+" ELSE "+absent+" END")
	}
	return strings.Join(parts, ",")
}

func (r authoringRepository) draftLock(ctx context.Context, tenant, iteration string) error {
	if err := r.mutating(); err != nil {
		return err
	}
	args, err := r.ids(tenant, iteration)
	if err != nil {
		return err
	}
	var status string
	if err := r.tx.QueryRowContext(ctx, "SELECT status FROM "+r.schema+".decision_engine_scenario_iterations WHERE tenant_id=? AND id=? FOR SHARE", args...).Scan(&status); err != nil {
		return err
	}
	if status != "draft" {
		return ErrCommittedIteration
	}
	return nil
}

func nullUUID(v *string) (mysqlstore.NullUUID, error) {
	if v == nil {
		return mysqlstore.NullUUID{}, nil
	}
	id, err := uuid.Parse(*v)
	if err != nil || id == uuid.Nil {
		return mysqlstore.NullUUID{}, ErrTenantScope
	}
	return mysqlstore.NullUUID{UUID: mysqlstore.UUID(id), Valid: true}, nil
}
func uuidPointer(v mysqlstore.NullUUID) *string {
	if !v.Valid {
		return nil
	}
	s := uuid.UUID(v.UUID).String()
	return &s
}
