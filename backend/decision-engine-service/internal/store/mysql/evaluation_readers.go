package mysql

import (
	"context"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/snooze"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

// EvaluationReaders owns configured stores, never retained SQL transactions.
// Every read uses its real repository, bounded projection and authorization.
// There are no embedded nil repositories or fake successful mutation methods.
type EvaluationReaders struct {
	configuration *ConfigurationStore
	history       *DecisionStore
	platform      *PlatformStore
	lists         *CustomListStore
}

func sameEvaluationScope(a, b *PlatformStore) bool {
	return a != nil && b != nil && a.db == b.db && a.schema == b.schema && a.tenant == b.tenant && a.maxRows == b.maxRows && a.maxBytes == b.maxBytes
}
func NewEvaluationReaders(owner *ExecutionStore, configuration *ConfigurationStore, history *DecisionStore, platform *PlatformStore, lists *CustomListStore) (EvaluationReaders, error) {
	if owner == nil || configuration == nil || configuration.authoring == nil || history == nil || lists == nil || lists.configuration == nil {
		return EvaluationReaders{}, ErrDecisionAuthorizationUnavailable
	}
	c := owner.configuration
	if !sameEvaluationScope(c, configuration.authoring.configuration) || !sameEvaluationScope(c, history.configuration) || !sameEvaluationScope(c, platform) || !sameEvaluationScope(c, lists.configuration.configuration) {
		return EvaluationReaders{}, ErrTenantScope
	}
	return EvaluationReaders{configuration, history, platform, lists}, nil
}
func (r EvaluationReaders) MatchesExecutionStore(owner *ExecutionStore) bool {
	return owner != nil && r.configuration != nil && r.configuration.authoring != nil && sameEvaluationScope(owner.configuration, r.configuration.authoring.configuration) && r.history != nil && sameEvaluationScope(owner.configuration, r.history.configuration) && sameEvaluationScope(owner.configuration, r.platform) && r.lists != nil && r.lists.configuration != nil && sameEvaluationScope(owner.configuration, r.lists.configuration.configuration)
}
func (r EvaluationReaders) Workflows() ports.EvaluationWorkflows {
	return evaluationWorkflows{r.configuration}
}
func (r EvaluationReaders) WorkflowRules() ports.EvaluationWorkflowRules {
	return evaluationWorkflowRules{r.configuration}
}
func (r EvaluationReaders) ScreeningConfigs() ports.EvaluationScreeningConfigs {
	return evaluationScreeningConfigs{r.configuration}
}
func (r EvaluationReaders) ScoringConfigs() ports.EvaluationScoringConfigs {
	return evaluationScoringConfigs{r.configuration}
}
func (r EvaluationReaders) Snoozes() ports.EvaluationSnoozes { return evaluationSnoozes{r.history} }
func (r EvaluationReaders) RecordTags() ports.EvaluationRecordTags {
	return evaluationPlatform{r.platform}
}
func (r EvaluationReaders) RiskSnapshots() ports.EvaluationRiskSnapshots {
	return evaluationPlatform{r.platform}
}
func (r EvaluationReaders) IPFlags() ports.EvaluationIPFlags         { return evaluationPlatform{r.platform} }
func (r EvaluationReaders) CustomLists() ports.EvaluationCustomLists { return evaluationLists{r.lists} }

func configurationEvaluationRead[T any](ctx context.Context, store *ConfigurationStore, read func(ConfigurationRepositories) (T, error)) (out T, err error) {
	err = store.ReadSnapshot(ctx, func(r ConfigurationRepositories) error { out, err = read(r); return err })
	if err != nil {
		var zero T
		return zero, err
	}
	return out, nil
}

type evaluationWorkflows struct{ store *ConfigurationStore }

func (r evaluationWorkflows) ListActiveByScenario(ctx context.Context, t, s string) ([]workflow.Definition, error) {
	return configurationEvaluationRead(ctx, r.store, func(v ConfigurationRepositories) ([]workflow.Definition, error) {
		return v.Workflows.ListActiveByScenario(ctx, t, s)
	})
}

type evaluationWorkflowRules struct{ store *ConfigurationStore }

func (r evaluationWorkflowRules) ListStructured(ctx context.Context, t, s string) ([]workflow.StructuredRule, error) {
	return configurationEvaluationRead(ctx, r.store, func(v ConfigurationRepositories) ([]workflow.StructuredRule, error) {
		return v.WorkflowRules.ListStructured(ctx, t, s)
	})
}
func (r evaluationWorkflowRules) ListByScenario(ctx context.Context, t, s string) ([]workflow.Rule, error) {
	return configurationEvaluationRead(ctx, r.store, func(v ConfigurationRepositories) ([]workflow.Rule, error) {
		return v.WorkflowRules.ListByScenario(ctx, t, s)
	})
}

type evaluationScreeningConfigs struct{ store *ConfigurationStore }

func (r evaluationScreeningConfigs) ListActiveByScenario(ctx context.Context, t, s string) ([]screening.Config, error) {
	return configurationEvaluationRead(ctx, r.store, func(v ConfigurationRepositories) ([]screening.Config, error) {
		return v.ScreeningConfigs.ListActiveByScenario(ctx, t, s)
	})
}

type evaluationScoringConfigs struct{ store *ConfigurationStore }

func (r evaluationScoringConfigs) ListActiveByScenario(ctx context.Context, t, s string) ([]scoring.Config, error) {
	return configurationEvaluationRead(ctx, r.store, func(v ConfigurationRepositories) ([]scoring.Config, error) {
		return v.ScoringConfigs.ListActiveByScenario(ctx, t, s)
	})
}

type evaluationSnoozes struct{ store *DecisionStore }

func (r evaluationSnoozes) ListActive(ctx context.Context, t, s, kind, id string, at time.Time) (out []snooze.RuleSnooze, err error) {
	err = r.store.ReadSnapshot(ctx, func(v DecisionRepositories) error {
		out, err = v.RuleSnoozes.ListActive(ctx, t, s, kind, id, at)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

type evaluationPlatform struct{ store *PlatformStore }

func (r evaluationPlatform) HasTag(ctx context.Context, t, kind, id, tag string) (out bool, err error) {
	err = r.store.ReadSnapshot(ctx, func(v PlatformRepositories) error { out, err = v.RecordTags.HasTag(ctx, t, kind, id, tag); return err })
	if err != nil {
		return false, err
	}
	return out, nil
}
func (r evaluationPlatform) GetByObject(ctx context.Context, t, kind, id string) (out *platform.RiskSnapshot, err error) {
	err = r.store.ReadSnapshot(ctx, func(v PlatformRepositories) error {
		out, err = v.RiskSnapshots.GetByObject(ctx, t, kind, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (r evaluationPlatform) HasFlag(ctx context.Context, t, ip, flag string) (out bool, err error) {
	err = r.store.ReadSnapshot(ctx, func(v PlatformRepositories) error { out, err = v.IPFlags.HasFlag(ctx, t, ip, flag); return err })
	if err != nil {
		return false, err
	}
	return out, nil
}

type evaluationLists struct{ store *CustomListStore }

func listEvaluationRead[T any](ctx context.Context, store *CustomListStore, read func(CustomListRepository) (T, error)) (out T, err error) {
	err = store.ReadSnapshot(ctx, func(r CustomListRepository) error { out, err = read(r); return err })
	if err != nil {
		var zero T
		return zero, err
	}
	return out, nil
}
func (r evaluationLists) GetListByID(ctx context.Context, t, id string) (platform.CustomList, error) {
	return listEvaluationRead(ctx, r.store, func(v CustomListRepository) (platform.CustomList, error) { return v.GetListByID(ctx, t, id) })
}
func (r evaluationLists) ListEntriesByListID(ctx context.Context, t, id string) ([]platform.CustomListEntry, error) {
	return listEvaluationRead(ctx, r.store, func(v CustomListRepository) ([]platform.CustomListEntry, error) {
		return v.ListEntriesByListID(ctx, t, id)
	})
}
func (r evaluationLists) ListByName(ctx context.Context, t, name string) ([]platform.CustomListEntry, error) {
	return listEvaluationRead(ctx, r.store, func(v CustomListRepository) ([]platform.CustomListEntry, error) { return v.ListByName(ctx, t, name) })
}
func (r evaluationLists) Contains(ctx context.Context, t, name, value string) (bool, error) {
	return listEvaluationRead(ctx, r.store, func(v CustomListRepository) (bool, error) { return v.Contains(ctx, t, name, value) })
}
