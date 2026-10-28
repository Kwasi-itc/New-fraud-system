package mysql

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var ErrPhysicalPlanningUnsupported = errors.New("physical operation requires an additional enforcement or migration protocol")

// MappedPhysicalField must come from the authorized tenant/table metadata and
// registry snapshot. The planner checks identity/shape but does not authorize it.
type MappedPhysicalField struct {
	Field      datamodel.Field
	Components []PhysicalField
}

type plannedField struct {
	field   datamodel.Field
	columns map[string]string
}
type PhysicalPlanner struct {
	database, schema, name, qualified string
	fields                            map[uuid.UUID]plannedField
}

// PhysicalStatement can only be constructed by typed planner methods. Reference
// definitions are captured separately on an isolated reference, not learned by
// accepting the shape of an application target.
type PhysicalStatement struct {
	step         migrations.Step
	requireEmpty bool
	native       PhysicalNativeChange
}

func (s PhysicalStatement) ReferenceStep() migrations.Step { return s.step }

// NewPhysicalPlanner retains and validates archived mappings in the complete
// snapshot. Typed field mutations select active fields only; archiving does not
// rename or remove physical columns, or prevent work on other active fields.
func NewPhysicalPlanner(database string, tenant uuid.UUID, table PhysicalTable, fields []MappedPhysicalField) (*PhysicalPlanner, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	name, err := mysqlstore.Identifier(table.Name)
	if err != nil {
		return nil, err
	}
	if tenant == uuid.Nil || table.TableID == uuid.Nil || !strings.HasPrefix(table.Name, "tenant_") || !strings.HasSuffix(table.Name, "_"+physicalUUID(table.TableID)) || len(fields) == 0 || len(fields) > 1000 {
		return nil, errors.New("registered table and 1..1000 scoped fields required")
	}
	p := &PhysicalPlanner{database: database, schema: schema, name: table.Name, qualified: schema + "." + name, fields: make(map[uuid.UUID]plannedField, len(fields))}
	names := map[string]bool{}
	for _, mapped := range fields {
		f := mapped.Field
		if f.ID == uuid.Nil || f.TenantID != tenant || f.TableID != table.TableID {
			return nil, ErrPhysicalConflict
		}
		if _, ok := p.fields[f.ID]; ok {
			return nil, errors.New("duplicate field identity")
		}
		if _, err := datamodel.ParseDataType(string(f.DataType)); err != nil {
			return nil, err
		}
		components := []string{"value"}
		if f.DataType == datamodel.DataTypeIPAddress {
			components = []string{"address", "family", "prefix"}
		}
		if len(mapped.Components) != len(components) {
			return nil, errors.New("incomplete physical field mapping")
		}
		columns := make(map[string]string, len(components))
		for _, c := range mapped.Components {
			if c.FieldID != f.ID || !strings.HasPrefix(c.Name, "field_") || !strings.HasSuffix(c.Name, "_"+physicalUUID(f.ID)+"_"+c.Component) {
				return nil, ErrPhysicalConflict
			}
			valid := false
			for _, want := range components {
				if c.Component == want {
					valid = true
				}
			}
			if !valid || columns[c.Component] != "" || names[c.Name] {
				return nil, errors.New("duplicate or invalid physical component")
			}
			quoted, err := mysqlstore.Identifier(c.Name)
			if err != nil {
				return nil, err
			}
			columns[c.Component] = quoted
			names[c.Name] = true
		}
		p.fields[f.ID] = plannedField{f, columns}
	}
	return p, nil
}

func physicalUUID(id uuid.UUID) string { return strings.ReplaceAll(id.String(), "-", "") }

func (p *PhysicalPlanner) selectFields(ids []uuid.UUID) ([]plannedField, error) {
	if len(ids) == 0 || len(ids) > 1000 {
		return nil, errors.New("1..1000 distinct field IDs required")
	}
	seen := map[uuid.UUID]bool{}
	result := make([]plannedField, 0, len(ids))
	for _, id := range ids {
		f, ok := p.fields[id]
		if !ok || seen[id] || f.field.Archived {
			return nil, ErrPhysicalConflict
		}
		seen[id] = true
		result = append(result, f)
	}
	return result, nil
}

func (p *PhysicalPlanner) statement(action, sql string) PhysicalStatement {
	sum := sha256.Sum256([]byte(sql))
	return PhysicalStatement{step: migrations.Step{ID: fmt.Sprintf("%s_%x", action, sum[:16]), Kind: "TABLE", Name: p.name, SQL: sql}}
}

// AddFields adds a bounded group in one ALTER. Nullability is explicit. Boolean
// and IP checks enforce representable values; IP components are all-null or a
// complete family/address/prefix tuple, preserving host bits. Enum enforcement
// and long-text uniqueness must use their additional owner protocols.
func (p *PhysicalPlanner) AddFields(ids []uuid.UUID) (PhysicalStatement, error) {
	fields, err := p.selectFields(ids)
	if err != nil {
		return PhysicalStatement{}, err
	}
	clauses := make([]string, 0, len(fields)*3)
	components := 0
	for _, f := range fields {
		components += len(f.columns)
		if components > 1000 {
			return PhysicalStatement{}, errors.New("field addition exceeds 1000 physical columns")
		}
		if f.field.IsEnum {
			return PhysicalStatement{}, ErrPhysicalPlanningUnsupported
		}
		nullability := " NULL"
		if !f.field.Nullable {
			nullability = " NOT NULL"
		}
		add := func(component, typ string) {
			clauses = append(clauses, "ADD COLUMN "+f.columns[component]+" "+typ+nullability)
		}
		switch f.field.DataType {
		case datamodel.DataTypeBool:
			add("value", "TINYINT")
			clauses = append(clauses, "ADD CONSTRAINT `field_shape_"+physicalUUID(f.field.ID)+"` CHECK ("+f.columns["value"]+" IS NULL OR "+f.columns["value"]+" IN (0,1))")
		case datamodel.DataTypeInt:
			add("value", "INT")
		case datamodel.DataTypeFloat:
			add("value", "DOUBLE")
		case datamodel.DataTypeString:
			add("value", "LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin")
		case datamodel.DataTypeTimestamp:
			add("value", "DATETIME(6)")
		case datamodel.DataTypeIPAddress:
			add("address", "VARBINARY(16)")
			add("family", "TINYINT UNSIGNED")
			add("prefix", "TINYINT UNSIGNED")
			a, b, c := f.columns["address"], f.columns["family"], f.columns["prefix"]
			check := fmt.Sprintf("(%s IS NULL AND %s IS NULL AND %s IS NULL) OR (%s IS NOT NULL AND %s IS NOT NULL AND %s IS NOT NULL AND ((%s=4 AND OCTET_LENGTH(%s)=4 AND %s<=32) OR (%s=6 AND OCTET_LENGTH(%s)=16 AND %s<=128)))", a, b, c, a, b, c, b, a, c, b, a, c)
			clauses = append(clauses, "ADD CONSTRAINT `field_shape_"+physicalUUID(f.field.ID)+"` CHECK ("+check+")")
		default:
			return PhysicalStatement{}, ErrPhysicalPlanningUnsupported
		}
		if f.field.IsUnique {
			cols, err := nativeIndexColumns([]plannedField{f})
			if err != nil {
				return PhysicalStatement{}, err
			}
			clauses = append(clauses, "ADD UNIQUE INDEX `record_unique_"+physicalUUID(f.field.ID)+"` ("+strings.Join(cols, ",")+")")
		}
	}
	statement := p.statement("add_fields", "ALTER TABLE "+p.qualified+" "+strings.Join(clauses, ", "))
	statement.native = PhysicalNativeChange{Kind: "add_fields", FieldIDs: append([]uuid.UUID(nil), ids...)}
	// MySQL can synthesize implicit values when adding a NOT NULL column. A
	// populated table needs an explicit backfill protocol, never silent zero fill.
	for _, f := range fields {
		if !f.field.Nullable {
			statement.requireEmpty = true
		}
	}
	return statement, nil
}

// Native indexes use complete fixed-width components, never prefix uniqueness
// or digest-only equality. Text indexes require a separately reviewed strategy.
func nativeIndexColumns(fields []plannedField) ([]string, error) {
	var result []string
	for _, f := range fields {
		if f.field.DataType == datamodel.DataTypeString {
			return nil, ErrPhysicalPlanningUnsupported
		}
		if f.field.DataType == datamodel.DataTypeIPAddress {
			result = append(result, f.columns["family"], f.columns["address"], f.columns["prefix"])
		} else {
			result = append(result, f.columns["value"])
		}
	}
	// Every supported component is <=16 bytes. Sixteen parts fit below 3072 bytes.
	if len(result) > 16 {
		return nil, errors.New("native index exceeds sixteen key parts")
	}
	return result, nil
}

func (p *PhysicalPlanner) CreateIndex(indexID uuid.UUID, ids []uuid.UUID, unique bool) (PhysicalStatement, error) {
	if indexID == uuid.Nil {
		return PhysicalStatement{}, errors.New("stable index identity required")
	}
	fields, err := p.selectFields(ids)
	if err != nil {
		return PhysicalStatement{}, err
	}
	columns, err := nativeIndexColumns(fields)
	if err != nil {
		return PhysicalStatement{}, err
	}
	modifier := ""
	if unique {
		modifier = "UNIQUE "
	}
	return p.statement("create_index", "ALTER TABLE "+p.qualified+" ADD "+modifier+"INDEX `record_index_"+physicalUUID(indexID)+"` ("+strings.Join(columns, ",")+")").withNative(PhysicalNativeChange{Kind: "create_index", IndexID: indexID, FieldIDs: append([]uuid.UUID(nil), ids...), Unique: unique}), nil
}

func (p *PhysicalPlanner) DropIndex(indexID uuid.UUID) (PhysicalStatement, error) {
	if indexID == uuid.Nil {
		return PhysicalStatement{}, errors.New("stable index identity required")
	}
	return p.statement("drop_index", "ALTER TABLE "+p.qualified+" DROP INDEX `record_index_"+physicalUUID(indexID)+"`").withNative(PhysicalNativeChange{Kind: "drop_index", IndexID: indexID}), nil
}

// CreateFieldUniqueIndex uses the same immutable field-level name as AddFields.
// The field snapshot must be owner-authorized; metadata IsUnique is committed
// only after verified DDL. Duplicate data fails the ALTER, never weakens equality.
func (p *PhysicalPlanner) CreateFieldUniqueIndex(fieldID uuid.UUID) (PhysicalStatement, error) {
	fields, err := p.selectFields([]uuid.UUID{fieldID})
	if err != nil {
		return PhysicalStatement{}, err
	}
	columns, err := nativeIndexColumns(fields)
	if err != nil {
		return PhysicalStatement{}, err
	}
	return p.statement("create_field_unique", "ALTER TABLE "+p.qualified+" ADD UNIQUE INDEX `record_unique_"+physicalUUID(fieldID)+"` ("+strings.Join(columns, ",")+")").withNative(PhysicalNativeChange{Kind: "create_field_unique", FieldID: fieldID}), nil
}

// DropFieldUniqueIndex removes only this field's native unique index. Text
// uniqueness has separate exact-key state and must not use this native protocol.
// The owner still checks relationship dependencies and publishes metadata.
func (p *PhysicalPlanner) DropFieldUniqueIndex(fieldID uuid.UUID) (PhysicalStatement, error) {
	fields, err := p.selectFields([]uuid.UUID{fieldID})
	if err != nil {
		return PhysicalStatement{}, err
	}
	if _, err = nativeIndexColumns(fields); err != nil {
		return PhysicalStatement{}, err
	}
	return p.statement("drop_field_unique", "ALTER TABLE "+p.qualified+" DROP INDEX `record_unique_"+physicalUUID(fieldID)+"`").withNative(PhysicalNativeChange{Kind: "drop_field_unique", FieldID: fieldID}), nil
}

// Bind refuses missing, reordered or substituted reference SQL. It does not
// manufacture desired definitions by inspecting a potentially drifted target.
func (p *PhysicalPlanner) Bind(statements []PhysicalStatement, reference []migrations.Step) (PhysicalDDLPlan, error) {
	if len(statements) == 0 || len(statements) > 1000 || len(statements) != len(reference) {
		return PhysicalDDLPlan{}, errors.New("complete bounded reference chain required")
	}
	steps := make([]migrations.Step, len(statements))
	var emptyBefore []string
	for i, statement := range statements {
		s, r := statement.step, reference[i]
		if s.ID == "" || s.Name != p.name || s.ID != r.ID || s.Kind != r.Kind || s.Name != r.Name || s.SQL != r.SQL {
			return PhysicalDDLPlan{}, errors.New("physical reference differs from typed operation")
		}
		steps[i] = s
		steps[i].Before = r.Before
		steps[i].After = r.After
		if statement.requireEmpty {
			emptyBefore = append(emptyBefore, s.ID)
		}
	}
	data, err := json.Marshal(PhysicalDDLPlan{Version: 1, Steps: steps, EmptyTableBefore: emptyBefore})
	if err != nil {
		return PhysicalDDLPlan{}, err
	}
	return decodePhysicalPlan(data, p.database, p.name)
}
