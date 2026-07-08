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
	required, relatedJobs, err := s.indexPreparationState(ctx, tenantID, scenarioID, iterationID)
	if err != nil {
		return scenario.PublicationPreparationStatus{}, err
	}
	status := scenario.PublicationPreparationStatus{
		ScenarioID:          scenarioID,
		IterationID:         iterationID,
		PreparationFinished: true,
	}
	applied := map[string]struct{}{}
	for _, job := range relatedJobs {
		if job.Status == "applied" || job.Status == "cancelled" {
			applied[indexRequirementKey(job.TableName, job.Columns)] = struct{}{}
		}
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
	for _, requirement := range required {
		if _, ok := applied[requirement.key()]; ok {
			continue
		}
		if !hasIndexJobForRequirement(relatedJobs, requirement) {
			status.PreparationRequired = true
			status.PreparationFinished = false
			status.PendingItems++
		}
	}
	return status, nil
}

func (s PublicationService) StartPreparation(ctx context.Context, tenantID, scenarioID, iterationID string) (scenario.PublicationPreparationStatus, error) {
	required, jobs, err := s.indexPreparationState(ctx, tenantID, scenarioID, iterationID)
	if err != nil {
		return scenario.PublicationPreparationStatus{}, err
	}
	for _, requirement := range required {
		if hasIndexJobForRequirement(jobs, requirement) {
			continue
		}
		job, err := s.dataModelReader.CreateIndexJob(ctx, tenantID, requirement.TableID, "search", requirement.Columns, "scenario_publication_preparation")
		if err != nil {
			return scenario.PublicationPreparationStatus{}, err
		}
		jobs = append(jobs, job)
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

type indexRequirement struct {
	TableID   string
	TableName string
	Columns   []string
}

func (r indexRequirement) key() string {
	return indexRequirementKey(r.TableName, r.Columns)
}

func indexRequirementKey(tableName string, columns []string) string {
	return strings.TrimSpace(tableName) + ":" + strings.Join(columns, ",")
}

func hasIndexJobForRequirement(jobs []ports.ManagedIndexJob, requirement indexRequirement) bool {
	requirementKey := requirement.key()
	for _, job := range jobs {
		if indexRequirementKey(job.TableName, job.Columns) == requirementKey {
			return true
		}
	}
	return false
}

func (s PublicationService) indexPreparationState(ctx context.Context, tenantID, scenarioID, iterationID string) ([]indexRequirement, []ports.ManagedIndexJob, error) {
	scn, err := s.scenarioRepo.GetByID(ctx, tenantID, scenarioID)
	if err != nil {
		return nil, nil, err
	}
	iteration, err := s.iterationRepo.GetByID(ctx, tenantID, scenarioID, iterationID)
	if err != nil {
		return nil, nil, err
	}
	rules, err := s.ruleRepo.ListByIteration(ctx, tenantID, scenarioID, iterationID)
	if err != nil {
		return nil, nil, err
	}
	model, err := s.dataModelReader.GetTenantModel(ctx, tenantID)
	if err != nil {
		return nil, nil, err
	}
	tableNames, err := collectReferencedTables(model, scn.TriggerObjectType, iteration.TriggerFormula, rules)
	if err != nil {
		return nil, nil, err
	}
	requirements := []indexRequirement{}
	allJobs, err := s.dataModelReader.ListIndexJobs(ctx, tenantID)
	if err != nil {
		return nil, nil, err
	}
	filtered := make([]ports.ManagedIndexJob, 0, len(allJobs))
	for _, job := range allJobs {
		if _, ok := tableNames[job.TableName]; ok {
			filtered = append(filtered, job)
		}
	}
	return requirements, filtered, nil
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

func collectIndexRequirements(model ports.TenantModel, baseTable string, triggerFormula json.RawMessage, rules []scenario.Rule) ([]indexRequirement, error) {
	seen := map[string]struct{}{}
	var requirements []indexRequirement
	add := func(requirement indexRequirement) {
		if len(requirement.Columns) == 0 {
			return
		}
		if _, ok := seen[requirement.key()]; ok {
			return
		}
		seen[requirement.key()] = struct{}{}
		requirements = append(requirements, requirement)
	}
	if err := collectFormulaIndexRequirements(model, baseTable, triggerFormula, add); err != nil {
		return nil, err
	}
	for _, rule := range rules {
		if err := collectFormulaIndexRequirements(model, baseTable, rule.Formula, add); err != nil {
			return nil, err
		}
	}
	return requirements, nil
}

func collectFormulaIndexRequirements(model ports.TenantModel, baseTable string, raw json.RawMessage, add func(indexRequirement)) error {
	if len(raw) == 0 {
		return nil
	}
	var node domainast.Node
	if err := json.Unmarshal(raw, &node); err != nil {
		return err
	}
	visitNodeIndexRequirements(model, baseTable, node, add)
	return nil
}

func visitNodeIndexRequirements(model ports.TenantModel, currentTable string, node domainast.Node, add func(indexRequirement)) {
	if node.Function == "Aggregator" {
		if requirement, ok := aggregatorIndexRequirement(model, currentTable, node); ok {
			add(requirement)
		}
	}
	for _, child := range node.Children {
		visitNodeIndexRequirements(model, currentTable, child, add)
	}
	for _, child := range node.NamedChildren {
		visitNodeIndexRequirements(model, currentTable, child, add)
	}
}

func aggregatorIndexRequirement(model ports.TenantModel, currentTable string, node domainast.Node) (indexRequirement, bool) {
	tableName, ok := constantString(node.NamedChildren["tableName"])
	if !ok || strings.TrimSpace(tableName) == "" {
		tableName = currentTable
	}
	table, ok := model.Tables[tableName]
	if !ok || strings.TrimSpace(table.ID) == "" {
		return indexRequirement{}, false
	}
	filtersNode, ok := node.NamedChildren["filters"]
	if !ok {
		return indexRequirement{}, false
	}
	columns := make([]string, 0, len(filtersNode.Children))
	seen := map[string]struct{}{}
	for _, filterNode := range filtersNode.Children {
		if filterNode.Function != "Filter" {
			continue
		}
		filterTableName, ok := constantString(filterNode.NamedChildren["tableName"])
		if ok && strings.TrimSpace(filterTableName) != "" && filterTableName != tableName {
			continue
		}
		fieldName, ok := constantString(filterNode.NamedChildren["fieldName"])
		if !ok || strings.TrimSpace(fieldName) == "" {
			continue
		}
		if _, exists := table.Fields[fieldName]; !exists {
			continue
		}
		if _, exists := seen[fieldName]; exists {
			continue
		}
		seen[fieldName] = struct{}{}
		columns = append(columns, fieldName)
	}
	if len(columns) == 0 {
		return indexRequirement{}, false
	}
	return indexRequirement{TableID: table.ID, TableName: table.Name, Columns: columns}, true
}

func visitNodeTables(model ports.TenantModel, currentTable string, node domainast.Node, out map[string]struct{}) {
	switch node.Function {
	case "Aggregator", "Filter":
		if tableName, ok := constantString(node.NamedChildren["tableName"]); ok && strings.TrimSpace(tableName) != "" {
			out[tableName] = struct{}{}
		}
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
