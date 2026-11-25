package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	asteval "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
)

const (
	defaultMaxSearchIndexesPerTable = 6
	defaultMaxRollupsPerTable        = 4
)

type RollupDefinitionRequest struct {
	TableName  string `json:"table_name"`
	GroupField string `json:"group_field"`
	TimeField  string `json:"time_field"`
	ValueField string `json:"value_field"`
}

type RollupDefinitionClient interface {
	EnsureRollupDefinition(ctx context.Context, tenantID string, req RollupDefinitionRequest) error
}

type PublicationService struct {
	txManager                   ports.TransactionManager
	idGen                       ports.IDGenerator
	clock                       ports.Clock
	publicationRepo             ports.ScenarioPublicationRepository
	scenarioRepo                ports.ScenarioRepository
	iterationRepo               ports.ScenarioIterationRepository
	ruleRepo                    ports.RuleRepository
	dataModelReader             ports.DataModelReader
	rollupClient                RollupDefinitionClient
	cacheInvalidator            DecisionMetadataCacheInvalidator
	aggregatePushdownMode       string
	aggregatePushdownAggregates []string
	maxSearchIndexesPerTable    int
	maxRollupsPerTable          int
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

func (s *PublicationService) SetCacheInvalidator(invalidator DecisionMetadataCacheInvalidator) {
	s.cacheInvalidator = invalidator
}

func (s *PublicationService) SetAggregatePushdownPolicy(mode string, aggregates []string) {
	s.aggregatePushdownMode = mode
	s.aggregatePushdownAggregates = append([]string(nil), aggregates...)
}

func (s *PublicationService) SetMaxSearchIndexesPerTable(limit int) {
	s.maxSearchIndexesPerTable = limit
}

func (s *PublicationService) SetRollupClient(client RollupDefinitionClient, maxPerTable int) {
	s.rollupClient = client
	s.maxRollupsPerTable = maxPerTable
}

func (s PublicationService) Publish(ctx context.Context, tenantID, scenarioID, iterationID string) ([]scenario.Publication, error) {
	// Owner preparation may use HTTP. Complete it before opening the mutation
	// transaction/current-authority locks; the committed iteration is rechecked below.
	status, err := s.GetPreparationStatus(ctx, tenantID, scenarioID, iterationID)
	if err != nil {
		return nil, err
	}
	if status.PreparationRequired {
		return nil, scenarioError(fmt.Sprintf("iteration requires data-model preparation for %d index jobs", status.PendingItems))
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
	applied := make([]ports.ManagedIndexJob, 0, len(relatedJobs))
	for _, job := range relatedJobs {
		if job.Status == "applied" {
			applied = append(applied, job)
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
		if requirementSatisfiedByJobs(applied, requirement) {
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
	maxPerTable := s.maxSearchIndexesPerTable
	if maxPerTable <= 0 {
		maxPerTable = defaultMaxSearchIndexesPerTable
	}
	activeCountByTable := map[string]int{}
	applied := make([]ports.ManagedIndexJob, 0, len(jobs))
	for _, job := range jobs {
		if job.Status == "applied" {
			applied = append(applied, job)
		}
		if job.IndexType == "search" && job.Status != "cancelled" && job.Status != "failed" {
			activeCountByTable[job.TableName]++
		}
	}
	for _, requirement := range required {
		if requirementSatisfiedByJobs(applied, requirement) || hasIndexJobForRequirement(jobs, requirement) {
			continue
		}
		if activeCountByTable[requirement.TableName] >= maxPerTable {
			return scenario.PublicationPreparationStatus{}, scenarioError(
				fmt.Sprintf("table %q exceeds managed search index cap (%d)", requirement.TableName, maxPerTable),
			)
		}
		var job ports.ManagedIndexJob
		if creator, ok := s.dataModelReader.(ports.CoveringIndexCreator); ok && len(requirement.IncludeColumns) > 0 {
			job, err = creator.CreateCoveringIndexJob(ctx, tenantID, requirement.TableID, "search", requirement.Columns, requirement.IncludeColumns, "scenario_publication_preparation")
		} else {
			job, err = s.dataModelReader.CreateIndexJob(ctx, tenantID, requirement.TableID, "search", requirement.Columns, "scenario_publication_preparation")
		}
		if err != nil {
			return scenario.PublicationPreparationStatus{}, err
		}
		jobs = append(jobs, job)
		activeCountByTable[requirement.TableName]++
	}
	for _, job := range jobs {
		if job.Status == "failed" {
			if err := s.dataModelReader.RetryIndexJob(ctx, tenantID, job.ID); err != nil {
				return scenario.PublicationPreparationStatus{}, err
			}
		}
	}
	status, err := s.GetPreparationStatus(ctx, tenantID, scenarioID, iterationID)
	if err != nil {
		return scenario.PublicationPreparationStatus{}, err
	}
	if s.rollupClient != nil {
		status.Warnings = append(status.Warnings, s.prepareRollupsNonBlocking(ctx, tenantID, scenarioID, iterationID)...)
	}
	return status, nil
}

func (s PublicationService) prepareRollupsNonBlocking(ctx context.Context, tenantID, scenarioID, iterationID string) []string {
	model, err := s.dataModelReader.GetTenantModel(ctx, tenantID)
	if err != nil {
		return nil
	}
	rules, err := s.ruleRepo.ListByIteration(ctx, tenantID, scenarioID, iterationID)
	if err != nil {
		return nil
	}
	maxRollups := s.maxRollupsPerTable
	if maxRollups <= 0 {
		maxRollups = defaultMaxRollupsPerTable
	}

	var warnings []string
	seen := map[string]struct{}{}
	countByTable := map[string]int{}
	for _, rule := range rules {
		var node domainast.Node
		if err := json.Unmarshal(rule.Formula, &node); err != nil {
			continue
		}
		for _, req := range extractEligibleRollupRequests(node, model) {
			key := req.TableName + "|" + req.GroupField + "|" + req.TimeField + "|" + req.ValueField
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			if countByTable[req.TableName] >= maxRollups {
				warnings = append(warnings, fmt.Sprintf("table %q exceeds per-table rollup cap (%d); falling back to covering index for %s", req.TableName, maxRollups, req.ValueField))
				continue
			}
			countByTable[req.TableName]++
			if err := s.rollupClient.EnsureRollupDefinition(ctx, tenantID, req); err != nil {
				warnings = append(warnings, fmt.Sprintf("rollup preparation warning for %s.%s: %v", req.TableName, req.ValueField, err))
			}
		}
	}
	return warnings
}

func extractEligibleRollupRequests(node domainast.Node, model ports.TenantModel) []RollupDefinitionRequest {
	var out []RollupDefinitionRequest
	var walk func(domainast.Node)
	walk = func(n domainast.Node) {
		if strings.EqualFold(strings.TrimSpace(n.Function), "Aggregator") {
			tableName, _ := n.NamedChildren["tableName"].Constant.(string)
			fieldName, _ := n.NamedChildren["fieldName"].Constant.(string)
			aggName, _ := n.NamedChildren["aggregator"].Constant.(string)
			aggLower := strings.ToLower(strings.TrimSpace(aggName))
			if aggLower == "count" || aggLower == "sum" || aggLower == "avg" {
				if table, ok := model.Tables[tableName]; ok {
					var eqField, timeField string
					var extractFilter func(domainast.Node) bool
					extractFilter = func(fn domainast.Node) bool {
						switch strings.ToLower(strings.TrimSpace(fn.Function)) {
						case "list", "and":
							for _, ch := range fn.Children {
								if !extractFilter(ch) {
									return false
								}
							}
							return true
						case "filter":
							fName, _ := fn.NamedChildren["fieldName"].Constant.(string)
							op, _ := fn.NamedChildren["operator"].Constant.(string)
							op = strings.ToLower(strings.TrimSpace(op))
							switch op {
							case "eq", "=":
								if eqField != "" {
									return false
								}
								eqField = fName
								return true
							case "gt", ">", "gte", ">=", "lt", "<", "lte", "<=":
								if timeField != "" && timeField != fName {
									return false
								}
								if tf, ok := table.Fields[fName]; ok && strings.Contains(strings.ToLower(tf.Type), "time") {
									timeField = fName
									return true
								}
								return false
							default:
								return false
							}
						default:
							return false
						}
					}
					if filterNode, ok := n.NamedChildren["filters"]; ok && extractFilter(filterNode) && eqField != "" && timeField != "" {
						out = append(out, RollupDefinitionRequest{
							TableName:  tableName,
							GroupField: eqField,
							TimeField:  timeField,
							ValueField: fieldName,
						})
					}
				}
			}
		}
		for _, ch := range n.Children {
			walk(ch)
		}
		for _, ch := range n.NamedChildren {
			walk(ch)
		}
	}
	walk(node)
	return out
}

type indexRequirement struct {
	TableID        string
	TableName      string
	Columns        []string
	IncludeColumns []string
}

func (r indexRequirement) key() string {
	return indexRequirementKey(r.TableName, r.Columns, r.IncludeColumns)
}

func indexRequirementKey(tableName string, columns, includeColumns []string) string {
	return strings.TrimSpace(tableName) + ":" + strings.Join(columns, ",") + "|inc:" + strings.Join(includeColumns, ",")
}

func requirementSatisfiedByJobs(applied []ports.ManagedIndexJob, req indexRequirement) bool {
	for _, job := range applied {
		if strings.TrimSpace(job.TableName) != strings.TrimSpace(req.TableName) || len(job.Columns) < len(req.Columns) {
			continue
		}
		prefixMatch := true
		for i, col := range req.Columns {
			if job.Columns[i] != col {
				prefixMatch = false
				break
			}
		}
		if !prefixMatch {
			continue
		}
		if len(req.IncludeColumns) == 0 {
			return true
		}
		covered := make(map[string]struct{}, len(job.Columns)+len(job.IncludeColumns))
		for _, c := range job.Columns {
			covered[c] = struct{}{}
		}
		for _, c := range job.IncludeColumns {
			covered[c] = struct{}{}
		}
		allIncluded := true
		for _, inc := range req.IncludeColumns {
			if _, ok := covered[inc]; !ok {
				allIncluded = false
				break
			}
		}
		if allIncluded {
			return true
		}
	}
	return false
}

func hasIndexJobForRequirement(jobs []ports.ManagedIndexJob, requirement indexRequirement) bool {
	requirementKey := requirement.key()
	for _, job := range jobs {
		if indexRequirementKey(job.TableName, job.Columns, job.IncludeColumns) == requirementKey {
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
	if len(s.aggregatePushdownMode) > 0 || len(s.aggregatePushdownAggregates) > 0 {
		if err := validateIterationAggregatorPolicy(iteration.TriggerFormula, rules, s.aggregatePushdownMode, s.aggregatePushdownAggregates); err != nil {
			return nil, nil, err
		}
	}
	model, err := s.dataModelReader.GetTenantModel(ctx, tenantID)
	if err != nil {
		return nil, nil, err
	}
	tableNames, err := collectReferencedTables(model, scn.TriggerObjectType, iteration.TriggerFormula, rules)
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

func validateIterationAggregatorPolicy(triggerFormula json.RawMessage, rules []scenario.Rule, mode string, allowed []string) error {
	checkFormula := func(raw json.RawMessage) error {
		if len(raw) == 0 {
			return nil
		}
		var node domainast.Node
		if err := json.Unmarshal(raw, &node); err != nil {
			return nil
		}
		if errs := asteval.ValidateAggregatorPolicy(node, mode, allowed); len(errs) > 0 {
			return scenarioError(strings.Join(errs, "; "))
		}
		return nil
	}
	if err := checkFormula(triggerFormula); err != nil {
		return err
	}
	for _, rule := range rules {
		if err := checkFormula(rule.Formula); err != nil {
			return err
		}
	}
	return nil
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
	topFilters := extractTopLevelAndFilters(filtersNode)
	equalitySet := map[string]struct{}{}
	var unspecifiedCols []string
	unspecifiedSet := map[string]struct{}{}
	rangeSet := map[string]struct{}{}
	extraFilterSet := map[string]struct{}{}

	for _, filterNode := range topFilters {
		filterTableName, ok := constantString(filterNode.NamedChildren["tableName"])
		if ok && strings.TrimSpace(filterTableName) != "" && filterTableName != tableName {
			continue
		}
		fieldName, ok := constantString(filterNode.NamedChildren["fieldName"])
		if !ok || strings.TrimSpace(fieldName) == "" {
			continue
		}
		if _, exists := table.Fields[fieldName]; !exists && fieldName != model.RecordLookupField {
			continue
		}
		opRaw, _ := constantString(filterNode.NamedChildren["operator"])
		switch strings.ToLower(strings.TrimSpace(opRaw)) {
		case "":
			if _, seen := unspecifiedSet[fieldName]; !seen {
				unspecifiedSet[fieldName] = struct{}{}
				unspecifiedCols = append(unspecifiedCols, fieldName)
			}
		case "=", "eq", "isinlist", "in":
			equalitySet[fieldName] = struct{}{}
		case ">", "gt", ">=", "gte", "<", "lt", "<=", "lte":
			rangeSet[fieldName] = struct{}{}
		default:
			extraFilterSet[fieldName] = struct{}{}
		}
	}

	equalityCols := make([]string, 0, len(equalitySet))
	for col := range equalitySet {
		equalityCols = append(equalityCols, col)
	}
	sort.Strings(equalityCols)
	for _, col := range unspecifiedCols {
		if _, inEq := equalitySet[col]; !inEq {
			equalityCols = append(equalityCols, col)
		}
	}

	// Select at most one range key, preferring TIMESTAMPTZ fields; remaining range fields go to INCLUDE.
	rangeCols := make([]string, 0, len(rangeSet))
	for col := range rangeSet {
		if _, inEq := equalitySet[col]; !inEq {
			rangeCols = append(rangeCols, col)
		}
	}
	sort.Strings(rangeCols)
	var chosenRange string
	for _, col := range rangeCols {
		if field, ok := table.Fields[col]; ok && strings.EqualFold(field.Type, "timestamp") {
			chosenRange = col
			break
		}
	}
	if chosenRange == "" && len(rangeCols) > 0 {
		chosenRange = rangeCols[0]
	}

	keyCols := make([]string, 0, len(equalityCols)+1)
	keyCols = append(keyCols, equalityCols...)
	if chosenRange != "" {
		keyCols = append(keyCols, chosenRange)
	}
	if len(keyCols) == 0 {
		return indexRequirement{}, false
	}

	keySet := make(map[string]struct{}, len(keyCols))
	for _, col := range keyCols {
		keySet[col] = struct{}{}
	}
	includeSet := map[string]struct{}{}
	for _, col := range rangeCols {
		if _, inKey := keySet[col]; !inKey {
			includeSet[col] = struct{}{}
		}
	}
	for col := range extraFilterSet {
		if _, inKey := keySet[col]; !inKey {
			includeSet[col] = struct{}{}
		}
	}
	if targetField, ok := constantString(node.NamedChildren["fieldName"]); ok {
		targetField = strings.TrimSpace(targetField)
		if targetField != "" {
			if _, exists := table.Fields[targetField]; exists {
				if _, inKey := keySet[targetField]; !inKey {
					includeSet[targetField] = struct{}{}
				}
			}
		}
	}
	includeCols := make([]string, 0, len(includeSet))
	for col := range includeSet {
		includeCols = append(includeCols, col)
	}
	sort.Strings(includeCols)

	return indexRequirement{
		TableID:        table.ID,
		TableName:      table.Name,
		Columns:        keyCols,
		IncludeColumns: includeCols,
	}, true
}

// extractTopLevelAndFilters flattens top-level List/and conjuncts and intentionally
// skips or/not subtrees so composite B-tree keys are never built from disjunctive branches.
func extractTopLevelAndFilters(node domainast.Node) []domainast.Node {
	switch strings.ToLower(strings.TrimSpace(node.Function)) {
	case "filter":
		return []domainast.Node{node}
	case "list", "and":
		var out []domainast.Node
		for _, child := range node.Children {
			out = append(out, extractTopLevelAndFilters(child)...)
		}
		return out
	default:
		return nil
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
