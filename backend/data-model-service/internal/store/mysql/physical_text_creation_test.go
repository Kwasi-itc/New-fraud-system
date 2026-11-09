package mysql

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/google/uuid"
)

func TestUniqueTextPlanAndSubsequentNativeChange(t *testing.T) {
	tenant, table, fields := creationFixture()
	p, err := NewPhysicalPlanner("shared_app", tenant, table, fields)
	if err != nil {
		t.Fatal(err)
	}
	definer := installation.Definer{User: "record_guard", Host: "localhost"}
	creation, err := p.CreateTableReference(fields[0].Field.ID, fields[1].Field.ID, definer)
	if err != nil {
		t.Fatal(err)
	}
	refs := creation.ReferenceSteps()
	for i := range refs {
		refs[i].After = `{"definition":"initial","type":"fixture"}`
	}
	previous, err := creation.Bind(refs)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	field := datamodel.Field{ID: id, TenantID: tenant, TableID: table.TableID, Name: "external_reference", DataType: datamodel.DataTypeString, IsUnique: true, Nullable: true}
	mapped, err := PreviewFieldMapping(field)
	if err != nil {
		t.Fatal(err)
	}
	fields = append(fields, mapped)
	p, err = NewPhysicalPlanner("shared_app", tenant, table, fields)
	if err != nil {
		t.Fatal(err)
	}
	statements, err := p.AddUniqueText(id, definer)
	if err != nil {
		t.Fatal(err)
	}
	refs = nil
	for i, s := range statements {
		r := s.ReferenceStep()
		r.After = `{"definition":"text","type":"fixture"}`
		if i == 0 {
			r.Before = previous.FinalTableDefinition()
		}
		refs = append(refs, r)
	}
	plan, err := p.BindRecordChanges(previous, statements, refs)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 3 || !plan.InstallsTextGuards() || len(plan.EmptyTableBefore) != 0 {
		t.Fatal(plan)
	}
	if !strings.Contains(plan.Steps[1].SQL, "CAST(NEW.") || !strings.Contains(plan.Steps[1].SQL, physicalUUID(tenant)+physicalUUID(id)) {
		t.Fatal("full-value scoped resolver missing")
	}
	for _, mutate := range []func(*PhysicalDDLPlan){
		func(p *PhysicalDDLPlan) { p.Steps = p.Steps[:2]; p.Native.Changes = p.Native.Changes[:2] },
		func(p *PhysicalDDLPlan) {
			p.Steps[1].SQL = strings.ReplaceAll(p.Steps[1].SQL, "derived_key", "forged_key")
		},
		func(p *PhysicalDDLPlan) { p.Native.Changes[1].FieldID = uuid.New() },
		func(p *PhysicalDDLPlan) { p.Steps[1].Before = `{"definition":"existing"}` },
		func(p *PhysicalDDLPlan) { p.Native.Changes[0].Nullable = true },
		func(p *PhysicalDDLPlan) { p.EmptyTableBefore = []string{p.Steps[0].ID} },
	} {
		raw, _ := json.Marshal(plan)
		var altered PhysicalDDLPlan
		_ = json.Unmarshal(raw, &altered)
		mutate(&altered)
		raw, _ = json.Marshal(altered)
		if _, err := decodePhysicalPlan(raw, "shared_app", table.Name); err == nil {
			t.Fatal("accepted altered text plan")
		}
	}
	nativeID := uuid.New()
	field.ID = nativeID
	field.Name = "score"
	field.DataType = datamodel.DataTypeInt
	field.IsUnique = false
	mapped, err = PreviewFieldMapping(field)
	if err != nil {
		t.Fatal(err)
	}
	fields = append(fields, mapped)
	p, err = NewPhysicalPlanner("shared_app", tenant, table, fields)
	if err != nil {
		t.Fatal(err)
	}
	native, err := p.AddFields([]uuid.UUID{nativeID})
	if err != nil {
		t.Fatal(err)
	}
	r := native.ReferenceStep()
	r.Before = plan.FinalTableDefinition()
	r.After = `{"definition":"native","type":"fixture"}`
	next, err := p.BindRecordChanges(plan, []PhysicalStatement{native}, []migrations.Step{r})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.RetainedGuards) != 4 || next.InstallsTextGuards() {
		t.Fatal("text guards not retained")
	}
	next.RetainedGuards = next.RetainedGuards[:3]
	raw, _ := json.Marshal(next)
	if _, err := decodePhysicalPlan(raw, "shared_app", table.Name); err == nil {
		t.Fatal("accepted missing retained text guard")
	}
	fields[2].Field.Nullable = false
	p, err = NewPhysicalPlanner("shared_app", tenant, table, fields)
	if err != nil {
		t.Fatal(err)
	}
	required, err := p.AddUniqueText(id, definer)
	if err != nil {
		t.Fatal(err)
	}
	if !required[0].requireEmpty {
		t.Fatal("required addition lacks empty table precondition")
	}
}

func TestTextGuardInstallerCapability(t *testing.T) {
	policy := installation.RecordInstallerPolicy{Principal: installation.Definer{User: "installer", Host: "localhost"}, Table: "tenant_records", NativeChanges: true}
	grants, err := policy.ActiveGrants("shared_app")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(grants, "\n"), "SET_ANY_DEFINER") {
		t.Fatal("native installer unexpectedly privileged")
	}
	policy.TextGuards = true
	grants, err = policy.ActiveGrants("shared_app")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(grants, "\n"), "SET_ANY_DEFINER") {
		t.Fatal("text installer missing transient capability")
	}
	inactive, err := policy.Account(true)
	if err != nil {
		t.Fatal(err)
	}
	grants, err = installation.GrantStatements("shared_app", inactive)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(grants, "\n"), "SET_ANY_DEFINER") {
		t.Fatal("inactive account retains global capability")
	}
}
