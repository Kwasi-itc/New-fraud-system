package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	asteval "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
)

type PublicationService struct {
	txManager       ports.TransactionManager
	idGen           ports.IDGenerator
	clock           ports.Clock
	publicationRepo ports.ScenarioPublicationRepository
	scenarioRepo    ports.ScenarioRepository
	iterationRepo   ports.ScenarioIterationRepository
	ruleRepo        ports.RuleRepository
	dataModelReader ports.DataModelReader
}

func NewPublicationService(
	txManager ports.TransactionManager,
	idGen ports.IDGenerator,
	clock ports.Clock,
	publicationRepo ports.ScenarioPublicationRepository,
	scenarioRepo ports.ScenarioRepository,
	iterationRepo ports.ScenarioIterationRepository,
	ruleRepo ports.RuleRepository,
	dataModelReader ports.DataModelReader,
) PublicationService {
	return PublicationService{
		txManager:       txManager,
		idGen:           idGen,
		clock:           clock,
		publicationRepo: publicationRepo,
		scenarioRepo:    scenarioRepo,
		iterationRepo:   iterationRepo,
		ruleRepo:        ruleRepo,
		dataModelReader: dataModelReader,
	}
}

func (s PublicationService) Publish(ctx context.Context, tenantID, scenarioID, iterationID string) ([]scenario.Publication, error) {
	var events []scenario.Publication
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		scn, err := store.Scenarios().GetByID(ctx, tenantID, scenarioID)
		if err != nil {
			return err
		}
		iteration, err := store.Iterations().GetByID(ctx, tenantID, scenarioID, iterationID)
		if err != nil {
			return err
		}
		if iteration.Status != scenario.IterationStatusCommitted {
			return scenarioError("iteration must be committed before publish")
		}

		status, err := s.GetPreparationStatus(ctx, tenantID, scenarioID, iterationID)
		if err != nil {
			return err
		}
		if status.PreparationRequired {
			return scenarioError(fmt.Sprintf("iteration requires data-model preparation for %d index jobs", status.PendingItems))
		}

		now := s.clock.Now()
		if scn.LiveIterationID != nil && *scn.LiveIterationID != iterationID {
			old := scenario.Publication{
				ID:          s.idGen.New().String(),
				TenantID:    tenantID,
				ScenarioID:  scenarioID,
				IterationID: *scn.LiveIterationID,
				Action:      scenario.PublicationActionUnpublish,
				CreatedAt:   now,
			}
			old, err = store.Publications().Create(ctx, old)
			if err != nil {
				return err
			}
			events = append(events, old)
		}

		if err := store.Scenarios().SetLiveIterationID(ctx, tenantID, scenarioID, &iterationID); err != nil {
			return err
		}

		pub := scenario.Publication{
			ID:          s.idGen.New().String(),
			TenantID:    tenantID,
			ScenarioID:  scenarioID,
			IterationID: iterationID,
			Action:      scenario.PublicationActionPublish,
			CreatedAt:   now,
		}
		pub, err = store.Publications().Create(ctx, pub)
		if err != nil {
			return err
		}
		events = append(events, pub)
		return nil
	})
	return events, err
}

func (s PublicationService) Unpublish(ctx context.Context, tenantID, scenarioID, iterationID string) ([]scenario.Publication, error) {
	var events []scenario.Publication
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		scn, err := store.Scenarios().GetByID(ctx, tenantID, scenarioID)
		if err != nil {
			return err
		}
		if scn.LiveIterationID == nil || *scn.LiveIterationID != iterationID {
			return scenarioError("iteration is not currently live")
		}
		if err := store.Scenarios().SetLiveIterationID(ctx, tenantID, scenarioID, nil); err != nil {
			return err
		}
		pub := scenario.Publication{
			ID:          s.idGen.New().String(),
			TenantID:    tenantID,
			ScenarioID:  scenarioID,
			IterationID: iterationID,
			Action:      scenario.PublicationActionUnpublish,
			CreatedAt:   s.clock.Now(),
		}
		pub, err = store.Publications().Create(ctx, pub)
		if err != nil {
			return err
		}
		events = append(events, pub)
		return nil
	})
	return events, err
}

func (s PublicationService) ListByScenario(ctx context.Context, tenantID, scenarioID string) ([]scenario.Publication, error) {
	return s.publicationRepo.ListByScenario(ctx, tenantID, scenarioID)
}

func (s PublicationService) GetPreparationStatus(ctx context.Context, tenantID, scenarioID, iterationID string) (scenario.PublicationPreparationStatus, error) {
	relatedJobs, err := s.listRelevantIndexJobs(ctx, tenantID, scenarioID, iterationID)
	if err != nil {
		return scenario.PublicationPreparationStatus{}, err
	}
	status := scenario.PublicationPreparationStatus{
		ScenarioID:          scenarioID,
		IterationID:         iterationID,
		PreparationFinished: true,
	}
	for _, job := range relatedJobs {
		switch job.Status {
		case "applied", "cancelled":
			continue
		case "running", "pending":
			status.PreparationRequired = true
			status.PreparationStarted = true
			status.PreparationFinished = false
			status.PendingItems++
		case "failed":
			status.PreparationRequired = true
			status.PreparationFinished = false
			status.PendingItems++
		default:
			status.PreparationRequired = true
			status.PreparationFinished = false
			status.PendingItems++
		}
	}
	return status, nil
}

func (s PublicationService) StartPreparation(ctx context.Context, tenantID, scenarioID, iterationID string) (scenario.PublicationPreparationStatus, error) {
	jobs, err := s.listRelevantIndexJobs(ctx, tenantID, scenarioID, iterationID)
	if err != nil {
		return scenario.PublicationPreparationStatus{}, err
	}
	for _, job := range jobs {
		if job.Status == "failed" {
			if err := s.dataModelReader.RetryIndexJob(ctx, job.ID); err != nil {
				return scenario.PublicationPreparationStatus{}, err
			}
		}
	}
	return s.GetPreparationStatus(ctx, tenantID, scenarioID, iterationID)
}

func (s PublicationService) listRelevantIndexJobs(ctx context.Context, tenantID, scenarioID, iterationID string) ([]ports.ManagedIndexJob, error) {
	scn, err := s.scenarioRepo.GetByID(ctx, tenantID, scenarioID)
	if err != nil {
		return nil, err
	}
	iteration, err := s.iterationRepo.GetByID(ctx, tenantID, scenarioID, iterationID)
	if err != nil {
		return nil, err
	}
	rules, err := s.ruleRepo.ListByIteration(ctx, tenantID, scenarioID, iterationID)
	if err != nil {
		return nil, err
	}
	model, err := s.dataModelReader.GetTenantModel(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	tableNames, err := collectReferencedTables(model, scn.TriggerObjectType, iteration.TriggerFormula, rules)
	if err != nil {
		return nil, err
	}
	if len(tableNames) == 0 {
		return []ports.ManagedIndexJob{}, nil
	}
	allJobs, err := s.dataModelReader.ListIndexJobs(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	filtered := make([]ports.ManagedIndexJob, 0, len(allJobs))
	for _, job := range allJobs {
		if _, ok := tableNames[job.TableName]; ok {
			filtered = append(filtered, job)
		}
	}
	return filtered, nil
}

func collectReferencedTables(model ports.TenantModel, baseTable string, triggerFormula json.RawMessage, rules []scenario.Rule) (map[string]struct{}, error) {
	result := map[string]struct{}{}
	if strings.TrimSpace(baseTable) != "" {
		result[baseTable] = struct{}{}
	}
	if err := collectFormulaTables(model, baseTable, triggerFormula, result); err != nil {
		return nil, err
	}
	for _, rule := range rules {
		if err := collectFormulaTables(model, baseTable, rule.Formula, result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func collectFormulaTables(model ports.TenantModel, baseTable string, raw json.RawMessage, out map[string]struct{}) error {
	if len(raw) == 0 {
		return nil
	}
	var node domainast.Node
	if err := json.Unmarshal(raw, &node); err != nil {
		return err
	}
	visitNodeTables(model, baseTable, node, out)
	return nil
}

func visitNodeTables(model ports.TenantModel, currentTable string, node domainast.Node, out map[string]struct{}) {
	switch node.Function {
	case "related_count":
		if target, ok := constantString(node.NamedChildren["object_type"]); ok && strings.TrimSpace(target) != "" {
			out[target] = struct{}{}
		}
	case "related_field":
		if path, ok := constantString(node.NamedChildren["path"]); ok && strings.TrimSpace(path) != "" {
			if targetTable, errs := asteval.ResolveRelatedPathTable(model, currentTable, path); len(errs) == 0 {
				out[targetTable.Name] = struct{}{}
			}
		}
	}
	for _, child := range node.Children {
		visitNodeTables(model, currentTable, child, out)
	}
	for _, child := range node.NamedChildren {
		visitNodeTables(model, currentTable, child, out)
	}
}

func constantString(node domainast.Node) (string, bool) {
	value, ok := node.Constant.(string)
	return value, ok
}
