package mysql

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/google/uuid"
)

// PhysicalTableCreation describes creation of the two mandatory model fields and
// record envelope. Bind requires isolated reference observations of all objects.
// The resulting v2 plan supports durable execution, not publication readiness.
type PhysicalTableCreation struct {
	steps    []migrations.Step
	policy   installation.AccountPolicy
	database string
	identity PhysicalCreationIdentity
}

// PhysicalCreationIdentity pins the typed generator inputs into the immutable
// intent. Decoding regenerates every statement, preventing substituted trigger
// bodies, principals, field mappings or cross-table SQL from entering v2 plans.
type PhysicalCreationIdentity struct {
	TenantID       uuid.UUID            `json:"tenant_id"`
	TableID        uuid.UUID            `json:"table_id"`
	ObjectFieldID  uuid.UUID            `json:"object_field_id"`
	ObjectColumn   string               `json:"object_column"`
	UpdatedFieldID uuid.UUID            `json:"updated_field_id"`
	UpdatedColumn  string               `json:"updated_column"`
	Definer        installation.Definer `json:"definer"`
}

func (c PhysicalTableCreation) Bind(reference []migrations.Step) (PhysicalDDLPlan, error) {
	if len(c.steps) != 3 || len(reference) != 3 {
		return PhysicalDDLPlan{}, errors.New("complete creation reference required")
	}
	steps := make([]migrations.Step, 3)
	for i, s := range c.steps {
		r := reference[i]
		if r.ID != s.ID || r.Kind != s.Kind || r.Name != s.Name || r.SQL != s.SQL || r.Before != "" {
			return PhysicalDDLPlan{}, errors.New("creation reference differs from typed operation")
		}
		steps[i] = r
	}
	identity := c.identity
	raw, err := json.Marshal(PhysicalDDLPlan{Version: 2, Steps: steps, Creation: &identity})
	if err != nil {
		return PhysicalDDLPlan{}, err
	}
	return decodePhysicalPlan(raw, c.database, c.steps[0].Name)
}

func validatePhysicalCreation(plan PhysicalDDLPlan, database, name string) error {
	if plan.Creation == nil || len(plan.Steps) != 3 || len(plan.EmptyTableBefore) != 0 {
		return errors.New("complete typed creation identity and three steps required")
	}
	c := plan.Creation
	fields := []MappedPhysicalField{
		{Field: datamodel.Field{ID: c.ObjectFieldID, TenantID: c.TenantID, TableID: c.TableID, Name: "object_id", DataType: datamodel.DataTypeString, IsUnique: true}, Components: []PhysicalField{{FieldID: c.ObjectFieldID, Name: c.ObjectColumn, Component: "value"}}},
		{Field: datamodel.Field{ID: c.UpdatedFieldID, TenantID: c.TenantID, TableID: c.TableID, Name: "updated_at", DataType: datamodel.DataTypeTimestamp}, Components: []PhysicalField{{FieldID: c.UpdatedFieldID, Name: c.UpdatedColumn, Component: "value"}}},
	}
	p, err := NewPhysicalPlanner(database, c.TenantID, PhysicalTable{TableID: c.TableID, Name: name}, fields)
	if err != nil {
		return err
	}
	expected, err := p.CreateTableReference(c.ObjectFieldID, c.UpdatedFieldID, c.Definer)
	if err != nil {
		return err
	}
	for i, want := range expected.steps {
		got := plan.Steps[i]
		if got.ID != want.ID || got.Kind != want.Kind || got.Name != want.Name || got.SQL != want.SQL || got.Before != "" {
			return errors.New("physical creation differs from typed source")
		}
	}
	return nil
}

func (c PhysicalTableCreation) ReferenceSteps() []migrations.Step {
	return append([]migrations.Step(nil), c.steps...)
}

// DefinerPolicy returns the complete grants for a dedicated locked trigger
// principal. The shared resolver has its own separately verified definer.
func (c PhysicalTableCreation) DefinerPolicy() installation.AccountPolicy {
	p := c.policy
	p.Objects = append([]installation.ObjectGrant(nil), p.Objects...)
	for i := range p.Objects {
		p.Objects[i].Privileges = append([]string(nil), p.Objects[i].Privileges...)
	}
	return p
}

// CreateTableReference requires the complete initial two-field snapshot. Extra
// fields must be planned separately; silently omitting them would lose data.
// Logical names are validated as the mandatory API fields, never used as SQL.
func (p *PhysicalPlanner) CreateTableReference(objectID, updatedAt uuid.UUID, definer installation.Definer) (PhysicalTableCreation, error) {
	account, err := definer.SQLAccount()
	if err != nil {
		return PhysicalTableCreation{}, err
	}
	fields, err := p.selectFields([]uuid.UUID{objectID, updatedAt})
	if err != nil {
		return PhysicalTableCreation{}, err
	}
	object, updated := fields[0], fields[1]
	if len(p.fields) != 2 || object.field.Name != "object_id" || object.field.DataType != datamodel.DataTypeString || object.field.Nullable || !object.field.IsUnique || object.field.IsEnum || updated.field.Name != "updated_at" || updated.field.DataType != datamodel.DataTypeTimestamp || updated.field.Nullable || updated.field.IsUnique || updated.field.IsEnum {
		return PhysicalTableCreation{}, errors.New("creation requires exactly the mandatory object_id and updated_at field contracts")
	}
	identity := physicalUUID(object.field.TableID)
	key := "`record_object_key_id`"
	tableSQL := "CREATE TABLE " + p.qualified + " (" +
		"`id` BINARY(16) NOT NULL PRIMARY KEY, " +
		object.columns["value"] + " LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NOT NULL, " +
		updated.columns["value"] + " DATETIME(6) NOT NULL, " +
		"`valid_from` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), " +
		"`valid_until` DATETIME(6) NULL DEFAULT NULL, " +
		key + " BINARY(16) NULL, " +
		"CONSTRAINT `record_object_key_present_" + identity + "` CHECK (" + key + " IS NOT NULL), " +
		"UNIQUE INDEX `record_unique_" + physicalUUID(objectID) + "` (" + key + "), " +
		"CONSTRAINT `record_object_key_ref_" + identity + "` FOREIGN KEY (" + key + ") REFERENCES " + p.schema + ".data_model_exact_keys(id)" +
		") ENGINE=InnoDB ROW_FORMAT=DYNAMIC"
	result := PhysicalTableCreation{policy: creationDefinerPolicy(p.name, definer)}
	result.database = p.database
	result.identity = PhysicalCreationIdentity{TenantID: object.field.TenantID, TableID: object.field.TableID, ObjectFieldID: objectID, ObjectColumn: object.columns["value"][1 : len(object.columns["value"])-1], UpdatedFieldID: updatedAt, UpdatedColumn: updated.columns["value"][1 : len(updated.columns["value"])-1], Definer: definer}
	step := func(kind, name, statement string) migrations.Step {
		sum := sha256.Sum256([]byte(statement))
		return migrations.Step{ID: fmt.Sprintf("create_record_%x", sum[:16]), Kind: kind, Name: name, SQL: statement}
	}
	result.steps = append(result.steps, step("TABLE", p.name, tableSQL))
	for _, event := range []struct{ sql, suffix string }{{"INSERT", "insert"}, {"UPDATE", "update"}} {
		name := "record_object_" + event.suffix + "_" + identity
		statement := "CREATE DEFINER=" + account + " TRIGGER " + p.schema + ".`" + name + "` BEFORE " + event.sql + " ON " + p.qualified + " FOR EACH ROW BEGIN\n" +
			"DECLARE derived_key BINARY(16);\n" +
			"IF @@session.unique_checks<>1 OR @@session.foreign_key_checks<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='record integrity checks must be enabled'; END IF;\n" +
			"CALL " + p.schema + ".data_model_resolve_exact_key(_binary'record_object_id',X'" + physicalUUID(object.field.TenantID) + identity + "',CAST(NEW." + object.columns["value"] + " AS BINARY),derived_key);\n" +
			"SET NEW." + key + "=derived_key;\nEND"
		result.steps = append(result.steps, step("TRIGGER", name, statement))
	}
	return result, nil
}
