package ports

import (
	"context"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/snooze"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
)

// Evaluation consumes reads, not authoring repositories. Splitting these
// capabilities prevents target composition from embedding nil mutation methods
// or pretending that a read facade implements a complete writable repository.
// Existing full repositories satisfy these interfaces unchanged.
type EvaluationWorkflows interface {
	ListActiveByScenario(context.Context, string, string) ([]workflow.Definition, error)
}
type EvaluationWorkflowRules interface {
	ListStructured(context.Context, string, string) ([]workflow.StructuredRule, error)
	ListByScenario(context.Context, string, string) ([]workflow.Rule, error)
}
type EvaluationScreeningConfigs interface {
	ListActiveByScenario(context.Context, string, string) ([]screening.Config, error)
}
type EvaluationScoringConfigs interface {
	ListActiveByScenario(context.Context, string, string) ([]scoring.Config, error)
}
type EvaluationSnoozes interface {
	ListActive(context.Context, string, string, string, string, time.Time) ([]snooze.RuleSnooze, error)
}
type EvaluationCustomLists interface {
	GetListByID(context.Context, string, string) (platform.CustomList, error)
	ListEntriesByListID(context.Context, string, string) ([]platform.CustomListEntry, error)
	ListByName(context.Context, string, string) ([]platform.CustomListEntry, error)
	Contains(context.Context, string, string, string) (bool, error)
}
type EvaluationRecordTags interface {
	HasTag(context.Context, string, string, string, string) (bool, error)
}
type EvaluationRiskSnapshots interface {
	GetByObject(context.Context, string, string, string) (*platform.RiskSnapshot, error)
}
type EvaluationIPFlags interface {
	HasFlag(context.Context, string, string, string) (bool, error)
}
