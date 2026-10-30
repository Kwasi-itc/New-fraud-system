package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// EffectStore stages decision-owned persistence, optionally with required jobs
// through NewQueuedEffectStore. It does not dispatch external work and is not
// the live TransactionManager. Nil reconciliation providers fail. Callbacks must
// use repositories serially, perform only local SQL and not retain them past return.
type EffectStore struct {
	configuration *PlatformStore
	screening     *ScreeningSourceContract
	queues        effectQueueConfig
	lists         *effectListAccess
}
type EffectRepositories struct {
	Decisions           DecisionRepository
	RuleExecutions      RuleExecutionRepository
	WorkflowExecutions  WorkflowExecutionRepository
	ScreeningExecutions ScreeningExecutionRepository
	ScoringRequests     ScoringRequestRepository
	OutboxEvents        OutboxEventRepository
	RecordTags          RecordTagRepository
	RiskSnapshots       RiskSnapshotRepository
	IPFlags             IPFlagRepository
	customLists         *CustomListRepository
}

func NewEffectStore(db *sql.DB, database string, tenant uuid.UUID, maxRows, maxBytes int, authorize PlatformAuthorizer, source *ScreeningSourceContract) (*EffectStore, error) {
	c, err := NewPlatformStore(db, database, tenant, maxRows, maxBytes, authorize)
	if err != nil {
		return nil, err
	}
	if source != nil && (source.schema != c.schema || source.maxBytes > c.maxBytes) {
		return nil, errors.New("same-database bounded screening contract required")
	}
	var copySource *ScreeningSourceContract
	if source != nil {
		v := *source
		copySource = &v
	}
	return &EffectStore{configuration: c, screening: copySource}, nil
}

// NewQueuedEffectStore requires all four implemented effect producers. Effect
// creation and every required initial delivery commit or roll back together.
// This remains a staged store: it neither starts workers nor implements the
// scheduled/async lifecycle or the complete live TransactionManager.
func NewQueuedEffectStore(db *sql.DB, database string, tenant uuid.UUID, maxRows, maxBytes int, authorize PlatformAuthorizer, source *ScreeningSourceContract, policies map[ports.JobKind]EffectQueuePolicy) (*EffectStore, error) {
	queues, err := copyEffectQueues(policies)
	if err != nil {
		return nil, err
	}
	s, err := NewEffectStore(db, database, tenant, maxRows, maxBytes, authorize, source)
	if err != nil {
		return nil, err
	}
	s.queues = queues
	return s, nil
}
func (s *EffectStore) Mutate(ctx context.Context, fn func(EffectRepositories) error) error {
	return s.run(ctx, true, fn)
}
func (s *EffectStore) ReadSnapshot(ctx context.Context, fn func(EffectRepositories) error) error {
	return s.run(ctx, false, fn)
}
func (s *EffectStore) run(ctx context.Context, write bool, fn func(EffectRepositories) error) error {
	if s == nil || s.configuration == nil || fn == nil {
		return errors.New("effect store and callback required")
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
		lists, err := s.authorizedCustomLists(ctx, tx, write)
		if err != nil {
			return err
		}
		var jobError error
		err = fn(s.repositories(tx, write, lists, &jobError))
		return errors.Join(err, jobError)
	})
}

// Caller has authorized every requested capability before acquiring owner locks.
// A shared error pointer makes required effect jobs part of an enclosing owner
// transaction as well as the standalone store's transaction.
func (s *EffectStore) repositories(tx *sql.Tx, write bool, lists *CustomListRepository, failed *error) EffectRepositories {
	c := s.configuration
	p := platformRepository{tx, c.schema, c.tenant, c.maxRows, c.maxBytes, write}
	r := authoringRepository{platformRepository: p, effectQueues: s.queues, effectJobError: failed}
	return EffectRepositories{Decisions: DecisionRepository{r}, RuleExecutions: RuleExecutionRepository{r}, WorkflowExecutions: WorkflowExecutionRepository{r}, ScreeningExecutions: ScreeningExecutionRepository{providerExecutionRepository{r, true}, s.screening}, ScoringRequests: ScoringRequestRepository{providerExecutionRepository{r, false}}, OutboxEvents: OutboxEventRepository{r}, RecordTags: RecordTagRepository{p}, RiskSnapshots: RiskSnapshotRepository{p}, IPFlags: IPFlagRepository{p}, customLists: lists}
}
