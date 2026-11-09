package mysql

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/google/uuid"
)

var managedKeyLine = regexp.MustCompile("^KEY `record_index_([a-f0-9]{32})` \\((.*)\\)$")
var managedColumn = regexp.MustCompile("`([a-z0-9_]+)`(?:\\([0-9]+\\))?")

// MissingManagedIndexes compares names only to select repair candidates. The
// complete before/after definition is independently checked by the executor.
// A wrong existing key, extra column, guard drift, etc. is never adopted.
func MissingManagedIndexes(source string, actual map[string]bool) ([]uuid.UUID, error) {
	keys, _, err := managedKeys(source)
	if err != nil {
		return nil, err
	}
	var result []uuid.UUID
	for _, key := range keys {
		if !actual["record_index_"+physicalUUID(key.id)] {
			result = append(result, key.id)
		}
	}
	return result, nil
}

type managedKey struct {
	id   uuid.UUID
	line string
}

func managedKeys(source string) ([]managedKey, map[string]string, error) {
	var definition map[string]string
	if err := json.Unmarshal([]byte(source), &definition); err != nil {
		return nil, nil, err
	}
	create := definition["Create Table"]
	if !strings.HasPrefix(create, "CREATE TABLE `tenant_") {
		return nil, nil, ErrPhysicalConflict
	}
	var keys []managedKey
	for _, line := range strings.Split(create, "\n") {
		line = strings.TrimSuffix(strings.TrimSpace(line), ",")
		m := managedKeyLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		id, err := uuid.Parse(m[1])
		if err != nil {
			return nil, nil, err
		}
		keys = append(keys, managedKey{id, line})
	}
	if len(keys) > 64 {
		return nil, nil, ErrSnapshotLimit
	}
	return keys, definition, nil
}

// ManagedRepairBefore derives the only allowed drift from a retained published
// definition: absence of explicitly identified, nonunique owner managed keys.
func ManagedRepairBefore(source string, missing []uuid.UUID) (string, error) {
	keys, definition, err := managedKeys(source)
	if err != nil {
		return "", err
	}
	if len(missing) == 0 || len(missing) > 64 {
		return "", ErrPhysicalConflict
	}
	wanted := map[uuid.UUID]bool{}
	for _, id := range missing {
		if id == uuid.Nil || wanted[id] {
			return "", ErrPhysicalConflict
		}
		wanted[id] = true
	}
	remove := map[string]bool{}
	for _, key := range keys {
		if wanted[key.id] {
			remove[key.line] = true
			delete(wanted, key.id)
		}
	}
	if len(wanted) != 0 {
		return "", ErrPhysicalConflict
	}
	var lines []string
	for _, line := range strings.Split(definition["Create Table"], "\n") {
		if !remove[strings.TrimSuffix(strings.TrimSpace(line), ",")] {
			lines = append(lines, line)
		}
	}
	for i := 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], ")") {
			lines[i-1] = strings.TrimSuffix(lines[i-1], ",")
		}
	}
	definition["Create Table"] = strings.Join(lines, "\n")
	raw, err := json.Marshal(definition)
	return string(raw), err
}

func (p *PhysicalPlanner) RestoreManagedIndexes(source string, missing []uuid.UUID) ([]PhysicalStatement, error) {
	keys, _, err := managedKeys(source)
	if err != nil {
		return nil, err
	}
	byColumn := map[string]uuid.UUID{}
	for id, f := range p.fields {
		for _, column := range f.columns {
			byColumn[strings.Trim(column, "`")] = id
		}
	}
	var result []PhysicalStatement
	for _, id := range missing {
		var line string
		for _, key := range keys {
			if key.id == id {
				line = key.line
			}
		}
		match := managedKeyLine.FindStringSubmatch(line)
		if match == nil {
			return nil, ErrPhysicalConflict
		}
		var ids []uuid.UUID
		for _, column := range managedColumn.FindAllStringSubmatch(match[2], -1) {
			field := byColumn[column[1]]
			if field == uuid.Nil {
				return nil, ErrPhysicalConflict
			}
			if len(ids) == 0 || ids[len(ids)-1] != field {
				ids = append(ids, field)
			}
		}
		statement, err := p.CreateManagedIndex(id, ids)
		if err != nil {
			return nil, err
		}
		_, key, ok := strings.Cut(statement.step.SQL, " ADD INDEX ")
		if !ok || "KEY "+key != line {
			return nil, ErrPhysicalConflict
		}
		result = append(result, statement)
	}
	return result, nil
}

func (p *PhysicalPlanner) BindManagedRepair(previous PhysicalDDLPlan, missing []uuid.UUID, statements []PhysicalStatement, reference []migrations.Step) (PhysicalDDLPlan, error) {
	raw, err := json.Marshal(previous)
	if err != nil {
		return PhysicalDDLPlan{}, err
	}
	original, err := decodePhysicalPlan(raw, p.database, p.name)
	if err != nil {
		return PhysicalDDLPlan{}, err
	}
	plan, err := p.bindGuardedReferences(statements, reference)
	if err != nil {
		return PhysicalDDLPlan{}, err
	}
	plan.Version, plan.Creation, plan.Native = 3, original.Creation, p.nativeSource(statements)
	plan.RetainedGuards = original.ActiveGuards()
	plan.RepairSource, plan.RepairMissing = original.FinalTableDefinition(), append([]uuid.UUID(nil), missing...)
	raw, err = json.Marshal(plan)
	if err != nil {
		return PhysicalDDLPlan{}, err
	}
	return decodePhysicalPlan(raw, p.database, p.name)
}

func validateManagedRepair(plan PhysicalDDLPlan, database, name string) error {
	if plan.RepairSource == "" && len(plan.RepairMissing) == 0 {
		return nil
	}
	if plan.Native == nil || plan.Creation == nil || len(plan.Steps) <= len(plan.RepairMissing) {
		return ErrPhysicalConflict
	}
	before, err := ManagedRepairBefore(plan.RepairSource, plan.RepairMissing)
	if err != nil {
		return err
	}
	if plan.Steps[0].Before != before {
		return errors.New("repair baseline differs from retained source minus missing managed keys")
	}
	p, err := NewPhysicalPlanner(database, plan.Creation.TenantID, PhysicalTable{TableID: plan.Creation.TableID, Name: name}, plan.Native.Fields)
	if err != nil {
		return err
	}
	restored, err := p.RestoreManagedIndexes(plan.RepairSource, plan.RepairMissing)
	if err != nil {
		return err
	}
	for i, statement := range restored {
		if plan.Steps[i].SQL != statement.step.SQL {
			return ErrPhysicalConflict
		}
	}
	for _, change := range plan.Native.Changes {
		if change.Kind != "create_managed_index" {
			return ErrPhysicalConflict
		}
	}
	return nil
}
