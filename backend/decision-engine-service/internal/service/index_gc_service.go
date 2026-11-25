package service

import (
	"context"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

const defaultIndexGCGracePeriod = 24 * time.Hour

type ManagedIndexDropper interface {
	DropManagedIndexJob(ctx context.Context, tenantID, jobID string) error
}

type IndexGCService struct {
	scenarioRepo    ports.ScenarioRepository
	iterationRepo   ports.ScenarioIterationRepository
	ruleRepo        ports.RuleRepository
	dataModelReader ports.DataModelReader
	dropper         ManagedIndexDropper
	clock           ports.Clock
	gracePeriod     time.Duration
}

func NewIndexGCService(
	scenarioRepo ports.ScenarioRepository,
	iterationRepo ports.ScenarioIterationRepository,
	ruleRepo ports.RuleRepository,
	dataModelReader ports.DataModelReader,
	dropper ManagedIndexDropper,
	clock ports.Clock,
	gracePeriod time.Duration,
) IndexGCService {
	if gracePeriod <= 0 {
		gracePeriod = defaultIndexGCGracePeriod
	}
	return IndexGCService{
		scenarioRepo:    scenarioRepo,
		iterationRepo:   iterationRepo,
		ruleRepo:        ruleRepo,
		dataModelReader: dataModelReader,
		dropper:         dropper,
		clock:           clock,
		gracePeriod:     gracePeriod,
	}
}

type IndexGCReport struct {
	CheckedJobs int
	RetainedIDs []string
	DroppedIDs  []string
}

// CollectUnreferencedAndDrop drops applied search index jobs only when zero
// live or committed iterations reference them (directly or via subsumption)
// and the configured grace period has elapsed since completion.
func (s IndexGCService) CollectUnreferencedAndDrop(ctx context.Context, tenantID string) (IndexGCReport, error) {
	model, err := s.dataModelReader.GetTenantModel(ctx, tenantID)
	if err != nil {
		return IndexGCReport{}, err
	}
	scenarios, err := s.scenarioRepo.ListByTenant(ctx, tenantID)
	if err != nil {
		return IndexGCReport{}, err
	}

	var requirements []indexRequirement
	for _, scn := range scenarios {
		iterations, err := s.iterationRepo.ListByScenario(ctx, tenantID, scn.ID)
		if err != nil {
			return IndexGCReport{}, err
		}
		for _, iter := range iterations {
			isLive := scn.LiveIterationID != nil && *scn.LiveIterationID == iter.ID
			if !isLive && iter.Status != scenario.IterationStatusCommitted {
				continue
			}
			rules, err := s.ruleRepo.ListByIteration(ctx, tenantID, scn.ID, iter.ID)
			if err != nil {
				return IndexGCReport{}, err
			}
			reqs, err := collectIndexRequirements(model, scn.TriggerObjectType, iter.TriggerFormula, rules)
			if err != nil {
				return IndexGCReport{}, err
			}
			requirements = append(requirements, reqs...)
		}
	}

	jobs, err := s.dataModelReader.ListIndexJobs(ctx, tenantID)
	if err != nil {
		return IndexGCReport{}, err
	}

	now := s.clock.Now()
	var report IndexGCReport
	for _, job := range jobs {
		if job.IndexType != "search" || job.Status != "applied" {
			continue
		}
		report.CheckedJobs++
		if jobReferencedByRequirements(job, requirements) {
			report.RetainedIDs = append(report.RetainedIDs, job.ID)
			continue
		}
		if job.CompletedAt == nil || now.Sub(*job.CompletedAt) < s.gracePeriod {
			report.RetainedIDs = append(report.RetainedIDs, job.ID)
			continue
		}
		if s.dropper != nil {
			if err := s.dropper.DropManagedIndexJob(ctx, tenantID, job.ID); err != nil {
				return report, err
			}
		}
		report.DroppedIDs = append(report.DroppedIDs, job.ID)
	}
	return report, nil
}

func jobReferencedByRequirements(job ports.ManagedIndexJob, requirements []indexRequirement) bool {
	jobKey := indexRequirementKey(job.TableName, job.Columns, job.IncludeColumns)
	single := []ports.ManagedIndexJob{job}
	for _, req := range requirements {
		if req.key() == jobKey || requirementSatisfiedByJobs(single, req) {
			return true
		}
	}
	return false
}
