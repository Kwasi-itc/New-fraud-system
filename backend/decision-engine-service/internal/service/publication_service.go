package service

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	asteval "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
)

type PublicationService struct {
	txManager         ports.TransactionManager
	idGen             ports.IDGenerator
	clock             ports.Clock
	publicationRepo   ports.ScenarioPublicationRepository
	scenarioRepo      ports.ScenarioRepository
	iterationRepo     ports.ScenarioIterationRepository
	ruleRepo          ports.RuleRepository
	dataModelReader   ports.DataModelReader
	factRegistry      ports.AggregateFactRegistry
	distributionCache *distributionSuggestionCache
	cacheInvalidator  DecisionMetadataCacheInvalidator
}

type distributionSuggestionCache struct {
	mu      sync.Mutex
	entries map[string]distributionSuggestionCacheEntry
}

type distributionSuggestionCacheEntry struct {
	analysis  ports.FieldDistributionSuggestion
	expiresAt time.Time
}

const distributionSuggestionCacheTTL = 5 * time.Minute

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
		txManager:         txManager,
		idGen:             idGen,
		clock:             clock,
		publicationRepo:   publicationRepo,
		scenarioRepo:      scenarioRepo,
		iterationRepo:     iterationRepo,
		ruleRepo:          ruleRepo,
		dataModelReader:   dataModelReader,
		distributionCache: &distributionSuggestionCache{entries: map[string]distributionSuggestionCacheEntry{}},
	}
}

func (s *PublicationService) SetCacheInvalidator(invalidator DecisionMetadataCacheInvalidator) {
	s.cacheInvalidator = invalidator
}

func (s *PublicationService) SetAggregateFactRegistry(registry ports.AggregateFactRegistry) {
	s.factRegistry = registry
}

func (s PublicationService) Publish(ctx context.Context, tenantID, scenarioID, iterationID string) ([]scenario.Publication, error) {
	iteration, err := s.iterationRepo.GetByID(ctx, tenantID, scenarioID, iterationID)
	if err != nil {
		return nil, err
	}
	if iteration.Status != scenario.IterationStatusCommitted {
		return nil, scenarioError("iteration must be committed before publish")
	}
	status, err := s.getPreparationStatus(ctx, tenantID, scenarioID, iterationID, false)
	if err != nil {
		return nil, err
	}
	if !status.PreparationFinished {
		return nil, scenarioError("publication preparation has not completed")
	}
	if err := s.syncAggregateFacts(ctx, tenantID, scenarioID, iterationID); err != nil {
		return nil, err
	}
	var events []scenario.Publication
	var triggerObjectType string
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		scn, err := store.Scenarios().GetByID(ctx, tenantID, scenarioID)
		if err != nil {
			return err
		}
		triggerObjectType = scn.TriggerObjectType
		iteration, err := store.Iterations().GetByID(ctx, tenantID, scenarioID, iterationID)
		if err != nil {
			return err
		}
		if iteration.Status != scenario.IterationStatusCommitted {
			return scenarioError("iteration must be committed before publish")
		}

		status, err := s.getPreparationStatus(ctx, tenantID, scenarioID, iterationID, false)
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
	if err == nil && s.cacheInvalidator != nil {
		s.cacheInvalidator.InvalidateScenario(ctx, tenantID, scenarioID)
		s.cacheInvalidator.InvalidateIteration(ctx, tenantID, scenarioID, iterationID)
		s.cacheInvalidator.InvalidateLiveScenariosByTriggerObject(ctx, tenantID, triggerObjectType)
	}
	return events, err
}

func (s PublicationService) Unpublish(ctx context.Context, tenantID, scenarioID, iterationID string) ([]scenario.Publication, error) {
	var events []scenario.Publication
	var triggerObjectType string
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		scn, err := store.Scenarios().GetByID(ctx, tenantID, scenarioID)
		if err != nil {
			return err
		}
		triggerObjectType = scn.TriggerObjectType
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
	if err == nil && s.cacheInvalidator != nil {
		s.cacheInvalidator.InvalidateScenario(ctx, tenantID, scenarioID)
		s.cacheInvalidator.InvalidateIteration(ctx, tenantID, scenarioID, iterationID)
		s.cacheInvalidator.InvalidateLiveScenariosByTriggerObject(ctx, tenantID, triggerObjectType)
	}
	if err == nil && s.factRegistry != nil {
		if registryErr := s.factRegistry.RemoveScenario(ctx, tenantID, scenarioID); registryErr != nil {
			return events, registryErr
		}
	}
	return events, err
}

func (s PublicationService) ListByScenario(ctx context.Context, tenantID, scenarioID string) ([]scenario.Publication, error) {
	return s.publicationRepo.ListByScenario(ctx, tenantID, scenarioID)
}

func (s PublicationService) GetPreparationStatus(ctx context.Context, tenantID, scenarioID, iterationID string) (scenario.PublicationPreparationStatus, error) {
	return s.getPreparationStatus(ctx, tenantID, scenarioID, iterationID, true)
}

func (s PublicationService) getPreparationStatus(ctx context.Context, tenantID, scenarioID, iterationID string, includeSuggestions bool) (scenario.PublicationPreparationStatus, error) {
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
		if job.Status == "applied" {
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
	if includeSuggestions {
		status.DistributionSuggestions = s.distributionSuggestions(ctx, tenantID, required)
	}
	return status, nil
}

func (s PublicationService) distributionSuggestions(ctx context.Context, tenantID string, requirements []indexRequirement) []scenario.DistributionSuggestion {
	reader, ok := s.dataModelReader.(ports.DistributionSuggestionReader)
	if !ok {
		return nil
	}
	model, err := s.dataModelReader.GetTenantModel(ctx, tenantID)
	if err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]scenario.DistributionSuggestion, 0)
	for _, requirement := range requirements {
		table, exists := model.Tables[requirement.TableName]
		if !exists {
			continue
		}
		for _, fieldName := range requirement.EqualityFields {
			field := table.Fields[fieldName]
			key := requirement.TableName + "|" + fieldName
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			analysis, err := s.analyzeStoredDistribution(ctx, reader, tenantID, requirement.TableName, fieldName)
			if err != nil || analysis.RowsAnalyzed == 0 {
				continue
			}
			out = append(out, scenario.DistributionSuggestion{
				TableName: requirement.TableName, FieldName: fieldName, AcceptedCategory: field.DistributionCategory,
				SuggestedCategory: analysis.SuggestedCategory, Reason: analysis.Reason,
				RowsAnalyzed: analysis.RowsAnalyzed, NonNullRows: analysis.NonNullRows,
				DistinctValues: analysis.DistinctValues, ExpectedSameValueRows: analysis.ExpectedSameValueRows,
				PolicyVersion: analysis.PolicyVersion,
			})
		}
	}
	return out
}

func (s PublicationService) analyzeStoredDistribution(ctx context.Context, reader ports.DistributionSuggestionReader, tenantID, tableName, fieldName string) (ports.FieldDistributionSuggestion, error) {
	key := tenantID + "|" + tableName + "|" + fieldName
	if s.distributionCache != nil {
		now := time.Now()
		s.distributionCache.mu.Lock()
		entry, ok := s.distributionCache.entries[key]
		s.distributionCache.mu.Unlock()
		if ok && now.Before(entry.expiresAt) {
			return entry.analysis, nil
		}
	}
	analysis, err := reader.AnalyzeStoredField(ctx, tenantID, tableName, fieldName)
	if err != nil {
		return ports.FieldDistributionSuggestion{}, err
	}
	if s.distributionCache != nil {
		s.distributionCache.mu.Lock()
		s.distributionCache.entries[key] = distributionSuggestionCacheEntry{
			analysis: analysis, expiresAt: time.Now().Add(distributionSuggestionCacheTTL),
		}
		s.distributionCache.mu.Unlock()
	}
	return analysis, nil
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
	if err := s.syncAggregateFacts(ctx, tenantID, scenarioID, iterationID); err != nil {
		return scenario.PublicationPreparationStatus{}, err
	}
	return s.GetPreparationStatus(ctx, tenantID, scenarioID, iterationID)
}

func (s PublicationService) syncAggregateFacts(ctx context.Context, tenantID, scenarioID, iterationID string) error {
	if s.factRegistry == nil {
		return nil
	}
	scn, err := s.scenarioRepo.GetByID(ctx, tenantID, scenarioID)
	if err != nil {
		return err
	}
	iteration, err := s.iterationRepo.GetByID(ctx, tenantID, scenarioID, iterationID)
	if err != nil {
		return err
	}
	rules, err := s.ruleRepo.ListByIteration(ctx, tenantID, scenarioID, iterationID)
	if err != nil {
		return err
	}
	model, err := s.dataModelReader.GetTenantModel(ctx, tenantID)
	if err != nil {
		return err
	}
	definitions, err := collectAggregateFactDefinitions(model, scn.TriggerObjectType, iteration.TriggerFormula, rules)
	if err != nil {
		return err
	}
	return s.factRegistry.SyncScenario(ctx, tenantID, scenarioID, iterationID, definitions)
}

type indexRequirement struct {
	TableID           string
	TableName         string
	Columns           []string
	EqualityFields    []string
	EventTimeField    string
	PrimarySelective  string
	SelectiveFields   []string
	DistributionAware bool
}

func (r indexRequirement) key() string {
	return indexRequirementKey(r.TableName, r.Columns)
}

func indexRequirementKey(tableName string, columns []string) string {
	return strings.TrimSpace(tableName) + ":" + strings.Join(columns, ",")
}

func hasIndexJobForRequirement(jobs []ports.ManagedIndexJob, requirement indexRequirement) bool {
	for _, job := range jobs {
		if job.Status == "cancelled" {
			continue
		}
		if indexJobCoversRequirement(job, requirement) {
			return true
		}
	}
	return false
}

func indexJobCoversRequirement(job ports.ManagedIndexJob, requirement indexRequirement) bool {
	if strings.TrimSpace(job.TableName) != strings.TrimSpace(requirement.TableName) {
		return false
	}
	if indexRequirementKey(job.TableName, job.Columns) == requirement.key() {
		return true
	}
	if requirement.DistributionAware {
		if requirement.EventTimeField == "" {
			return false
		}
		if requirement.PrimarySelective == "" {
			// Few-value facts share a time-led boundary index. An older
			// (merchant,date) index can answer one rule, but it must not stop
			// the shared (date) support path from being created.
			return len(job.Columns) > 0 && job.Columns[0] == requirement.EventTimeField
		}
		selectiveFields := requirement.SelectiveFields
		if len(selectiveFields) == 0 {
			selectiveFields = []string{requirement.PrimarySelective}
		}
		if len(job.Columns) < 2 || !slices.Contains(selectiveFields, job.Columns[0]) {
			return false
		}
		timePosition := slices.Index(job.Columns, requirement.EventTimeField)
		if timePosition < 1 {
			return false
		}
		requiredEquality := make(map[string]struct{}, len(requirement.EqualityFields))
		for _, field := range requirement.EqualityFields {
			requiredEquality[field] = struct{}{}
		}
		for _, field := range job.Columns[1:timePosition] {
			if _, ok := requiredEquality[field]; !ok {
				return false
			}
		}
		return true
	}
	if requirement.EventTimeField == "" || len(requirement.EqualityFields) == 0 {
		return false
	}
	timePosition := slices.Index(job.Columns, requirement.EventTimeField)
	if timePosition < 0 || timePosition != len(requirement.EqualityFields) {
		return false
	}
	jobEquality := append([]string(nil), job.Columns[:timePosition]...)
	requiredEquality := append([]string(nil), requirement.EqualityFields...)
	slices.Sort(jobEquality)
	slices.Sort(requiredEquality)
	return slices.Equal(jobEquality, requiredEquality)
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
	requirements, err := collectIndexRequirements(model, scn.TriggerObjectType, iteration.TriggerFormula, rules)
	if err != nil {
		return nil, nil, err
	}
	allJobs, err := s.dataModelReader.ListIndexJobs(ctx, tenantID)
	if err != nil {
		return nil, nil, err
	}
	return requirements, relevantIndexJobs(allJobs, requirements), nil
}

func relevantIndexJobs(jobs []ports.ManagedIndexJob, requirements []indexRequirement) []ports.ManagedIndexJob {
	filtered := make([]ports.ManagedIndexJob, 0, len(jobs))
	for _, job := range jobs {
		for _, requirement := range requirements {
			if indexJobCoversRequirement(job, requirement) {
				filtered = append(filtered, job)
				break
			}
		}
	}
	return filtered
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
	legacyColumns := make([]string, 0, len(filtersNode.Children))
	equalityFields := make([]string, 0, len(filtersNode.Children))
	eventTimeField := ""
	unsupportedShape := false
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
		field, exists := table.Fields[fieldName]
		if !exists {
			continue
		}
		if _, exists := seen[fieldName]; exists {
			continue
		}
		seen[fieldName] = struct{}{}
		legacyColumns = append(legacyColumns, fieldName)
		operator, _ := constantString(filterNode.NamedChildren["operator"])
		switch normalizeIndexOperator(operator) {
		case "eq":
			equalityFields = append(equalityFields, fieldName)
		case "gte", "gt", "lte", "lt":
			if isEventTimeFilter(filterNode, fieldName, field.Type) && eventTimeField == "" {
				eventTimeField = fieldName
			} else {
				unsupportedShape = true
			}
		default:
			unsupportedShape = true
		}
	}
	if len(legacyColumns) == 0 {
		return indexRequirement{}, false
	}
	legacy := indexRequirement{TableID: table.ID, TableName: table.Name, Columns: legacyColumns}
	if unsupportedShape || eventTimeField == "" || len(equalityFields) == 0 || !supportedFactAggregate(node) {
		return legacy, true
	}
	legacy.EqualityFields = append([]string(nil), equalityFields...)
	legacy.EventTimeField = eventTimeField

	selective := make([]ports.TenantModelField, 0, len(equalityFields))
	allKnown := true
	allFew := true
	for _, name := range equalityFields {
		field := table.Fields[name]
		if strings.TrimSpace(field.Name) == "" {
			field.Name = name
		}
		switch field.DistributionCategory {
		case "few_value_dominated":
		case "highly_distributed", "unique_or_near_unique":
			allFew = false
			selective = append(selective, field)
		default:
			allKnown = false
			allFew = false
		}
	}
	if !allKnown {
		return legacy, true
	}
	if allFew {
		return indexRequirement{
			TableID: table.ID, TableName: table.Name, Columns: []string{eventTimeField},
			EqualityFields: equalityFields, EventTimeField: eventTimeField, DistributionAware: true,
		}, true
	}
	if len(selective) == 0 {
		return legacy, true
	}
	slices.SortFunc(selective, func(left, right ports.TenantModelField) int {
		leftUnique := left.DistributionCategory == "unique_or_near_unique"
		rightUnique := right.DistributionCategory == "unique_or_near_unique"
		if leftUnique != rightUnique {
			if leftUnique {
				return -1
			}
			return 1
		}
		if left.ExpectedSameValueRows > 0 && right.ExpectedSameValueRows > 0 && left.ExpectedSameValueRows != right.ExpectedSameValueRows {
			if left.ExpectedSameValueRows < right.ExpectedSameValueRows {
				return -1
			}
			return 1
		}
		return strings.Compare(left.Name, right.Name)
	})
	primary := selective[0].Name
	selectiveFields := make([]string, 0, len(selective))
	for _, field := range selective {
		selectiveFields = append(selectiveFields, field.Name)
	}
	return indexRequirement{
		TableID: table.ID, TableName: table.Name, Columns: []string{primary, eventTimeField},
		EqualityFields: equalityFields, EventTimeField: eventTimeField,
		PrimarySelective: primary, SelectiveFields: selectiveFields, DistributionAware: true,
	}, true
}

func normalizeIndexOperator(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "=", "eq":
		return "eq"
	case ">", "gt":
		return "gt"
	case ">=", "gte":
		return "gte"
	case "<", "lt":
		return "lt"
	case "<=", "lte":
		return "lte"
	default:
		return ""
	}
}

func isEventTimeFilter(filterNode domainast.Node, fieldName, fieldType string) bool {
	if fieldType != "timestamp" {
		return false
	}
	valueNode, ok := filterNode.NamedChildren["value"]
	if !ok {
		return false
	}
	return strings.EqualFold(valueNode.Function, "TimeAdd") || strings.EqualFold(valueNode.Function, "time_add")
}

func supportedFactAggregate(node domainast.Node) bool {
	name, ok := constantString(node.NamedChildren["aggregator"])
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "count", "sum", "avg":
		return true
	default:
		return false
	}
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
