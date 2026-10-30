package mysql

import (
	"context"
	"database/sql"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type ConfigurationStore struct{ authoring *AuthoringStore }
type ConfigurationRepositories struct {
	Workflows          WorkflowRepository
	ScreeningConfigs   ScreeningConfigRepository
	ScoringConfigs     ScoringConfigRepository
	WorkflowRules      WorkflowRuleRepository
	WorkflowConditions WorkflowConditionRepository
	WorkflowActions    WorkflowActionRepository
}

func NewConfigurationStore(db *sql.DB, database string, tenant uuid.UUID, maxRows, maxBytes int, authorize AuthoringAuthorizer) (*ConfigurationStore, error) {
	s, err := NewAuthoringStore(db, database, tenant, maxRows, maxBytes, authorize)
	if err != nil {
		return nil, err
	}
	return &ConfigurationStore{s}, nil
}
func (s *ConfigurationStore) Mutate(ctx context.Context, fn func(ConfigurationRepositories) error) error {
	return s.run(ctx, true, fn)
}
func (s *ConfigurationStore) ReadSnapshot(ctx context.Context, fn func(ConfigurationRepositories) error) error {
	return s.run(ctx, false, fn)
}
func (s *ConfigurationStore) run(ctx context.Context, write bool, fn func(ConfigurationRepositories) error) error {
	if s == nil || s.authoring == nil || fn == nil {
		return errors.New("configuration store and callback required")
	}
	c := s.authoring.configuration
	run := mysqlstore.Snapshot
	if write {
		run = mysqlstore.Mutation
	}
	return run(ctx, c.db, func(tx *sql.Tx) error {
		if err := c.authorize(ctx, tx, c.tenant, write); err != nil {
			return err
		}
		r := authoringRepository{platformRepository: platformRepository{tx, c.schema, c.tenant, c.maxRows, c.maxBytes, write}, workflowGuards: make(map[uuid.UUID]bool)}
		return fn(ConfigurationRepositories{Workflows: WorkflowRepository{configurationRepository{r, workflowConfiguration}}, ScreeningConfigs: ScreeningConfigRepository{configurationRepository{r, screeningConfiguration}}, ScoringConfigs: ScoringConfigRepository{configurationRepository{r, scoringConfiguration}}, WorkflowRules: WorkflowRuleRepository{r}, WorkflowConditions: WorkflowConditionRepository{workflowChildRepository{r, false}}, WorkflowActions: WorkflowActionRepository{workflowChildRepository{r, true}}})
	})
}
