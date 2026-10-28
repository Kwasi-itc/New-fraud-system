package service

import (
	"crypto/sha256"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/google/uuid"
)

func requestTestScope() PublicationRequestContext {
	return PublicationRequestContext{
		TenantID:  uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		TableID:   uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		ActorID:   uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		Condition: contract.PublicationCondition{Mode: contract.PublicationExactVersion, Version: uuid.MustParse("55555555-5555-5555-5555-555555555555")},
	}
}

func TestPublicationRequestGolden(t *testing.T) {
	empty, no := "", false
	input := UpdateFieldInput{FieldID: uuid.MustParse("44444444-4444-4444-4444-444444444444"), Alias: &empty, Nullable: &no}
	want := `{"format":"dm-publication-request/v1","tenant_id":"11111111-1111-1111-1111-111111111111","table_id":"22222222-2222-2222-2222-222222222222","actor_id":"33333333-3333-3333-3333-333333333333","condition":{"mode":"exact","version":"55555555-5555-5555-5555-555555555555"},"kind":"field.update","body":{"field_id":"44444444-4444-4444-4444-444444444444","description":null,"alias":"","nullable":false,"is_enum":null,"is_unique":null}}`
	encoded, err := encodePublicationRequest(requestTestScope(), input)
	if err != nil || string(encoded) != want {
		t.Fatalf("v1 encoding changed: %s %v", encoded, err)
	}
	hash, err := PublicationRequestHash(requestTestScope(), input)
	if err != nil || hash != sha256.Sum256([]byte(want)) {
		t.Fatal("fingerprint not bound to pinned encoding", err)
	}
}

func TestPublicationRequestEveryInputField(t *testing.T) {
	scope := requestTestScope()
	inputs := []any{
		CreateNavigationOptionInput{TenantID: scope.TenantID, SourceTableID: scope.TableID, SourceFieldID: uuid.New(), TargetTableID: uuid.New(), FilterFieldID: uuid.New(), OrderingFieldID: uuid.New()},
		CreateTableInput{TenantID: scope.TenantID, Name: "records"},
		CreateLinkInput{TenantID: scope.TenantID, ChildTable: scope.TableID, ParentTable: uuid.New(), ParentField: uuid.New(), ChildField: uuid.New(), Name: "relation"},
		CreatePivotInput{TenantID: scope.TenantID, BaseTableID: scope.TableID},
		CreateIndexJobInput{TenantID: scope.TenantID, TableID: scope.TableID},
		UpdateTableInput{TableID: scope.TableID},
		CreateFieldInput{TableID: scope.TableID, Name: "state", DataType: datamodel.DataTypeString},
		UpdateFieldInput{FieldID: uuid.New()},
		CreateFieldEnumValueInput{FieldID: uuid.New(), Value: "a", Label: "A"},
		UpdateFieldEnumValueInput{EnumValueID: uuid.New()},
	}
	for _, input := range inputs {
		base, err := PublicationRequestHash(scope, input)
		if err != nil {
			t.Fatal(err)
		}
		typ := reflect.TypeOf(input)
		for i := 0; i < typ.NumField(); i++ {
			t.Run(typ.Name()+"/"+typ.Field(i).Name, func(t *testing.T) {
				changed := reflect.New(typ).Elem()
				changed.Set(reflect.ValueOf(input))
				f := changed.Field(i)
				modifiedScope := scope
				switch {
				case f.Type() == reflect.TypeOf(uuid.UUID{}):
					id := uuid.New()
					f.Set(reflect.ValueOf(id))
					if typ.Field(i).Name == "TableID" || typ.Field(i).Name == "BaseTableID" || typ.Field(i).Name == "ChildTable" || typ.Field(i).Name == "SourceTableID" {
						modifiedScope.TableID = id
					}
					if typ.Field(i).Name == "TenantID" {
						modifiedScope.TenantID = id
					}
				case f.Kind() == reflect.String:
					f.SetString(f.String() + "changed")
				case f.Kind() == reflect.Bool:
					f.SetBool(!f.Bool())
				case f.Kind() == reflect.Int:
					f.SetInt(f.Int() + 1)
				case f.Kind() == reflect.Pointer:
					p := reflect.New(f.Type().Elem())
					// Explicit empty/false/zero must differ from omitted/null.
					f.Set(p)
				case f.Type() == reflect.TypeOf([]CreateFieldEnumValueSeed{}):
					f.Set(reflect.ValueOf([]CreateFieldEnumValueSeed{{Value: "a", Label: "A", SortOrder: 1}}))
				case f.Type() == reflect.TypeOf([]uuid.UUID{}):
					f.Set(reflect.ValueOf([]uuid.UUID{uuid.New()}))
				case f.Type() == reflect.TypeOf([]string{}):
					f.Set(reflect.ValueOf([]string{"field"}))
				default:
					t.Fatalf("new input field needs encoding coverage: %s", f.Type())
				}
				hash, err := PublicationRequestHash(modifiedScope, changed.Interface())
				if err != nil || hash == base {
					t.Fatal("mutation field omitted from fingerprint", err)
				}
			})
		}
	}
}

func TestPublicationRequestCreationWire(t *testing.T) {
	scope := requestTestScope()
	id := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	for _, test := range []struct {
		kind  string
		input any
		body  string
	}{
		{"table.create", CreateTableInput{TenantID: scope.TenantID, Name: "Records"}, `{"tenant_id":"11111111-1111-1111-1111-111111111111","name":"Records","description":"","alias":"","semantic_type":""}`},
		{"link.create", CreateLinkInput{TenantID: scope.TenantID, ChildTable: scope.TableID, ParentTable: id, ParentField: id, ChildField: id, Name: "Parent"}, `{"tenant_id":"11111111-1111-1111-1111-111111111111","name":"Parent","parent_table_id":"44444444-4444-4444-4444-444444444444","parent_field_id":"44444444-4444-4444-4444-444444444444","child_table_id":"22222222-2222-2222-2222-222222222222","child_field_id":"44444444-4444-4444-4444-444444444444"}`},
		{"pivot.create", CreatePivotInput{TenantID: scope.TenantID, BaseTableID: scope.TableID, FieldID: &id}, `{"tenant_id":"11111111-1111-1111-1111-111111111111","base_table_id":"22222222-2222-2222-2222-222222222222","field_id":"44444444-4444-4444-4444-444444444444","path_link_ids":null}`},
		{"index_job.create", CreateIndexJobInput{TenantID: scope.TenantID, TableID: scope.TableID, Columns: []string{"a", "b"}}, `{"tenant_id":"11111111-1111-1111-1111-111111111111","table_id":"22222222-2222-2222-2222-222222222222","index_type":"","columns":["a","b"],"requested_by_operation":"","scheduled_at":null}`},
	} {
		t.Run(test.kind, func(t *testing.T) {
			raw, err := encodePublicationRequest(scope, test.input)
			if err != nil || !strings.HasSuffix(string(raw), `"kind":"`+test.kind+`","body":`+test.body+`}`) {
				t.Fatalf("creation wire changed: %s %v", raw, err)
			}
		})
	}
}

func TestPublicationRequestCreationOrderingAndTime(t *testing.T) {
	scope := requestTestScope()
	hash := func(input any) [32]byte {
		t.Helper()
		value, err := PublicationRequestHash(scope, input)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	pivot := CreatePivotInput{TenantID: scope.TenantID, BaseTableID: scope.TableID}
	base := hash(pivot)
	pivot.PathLinkIDs = []uuid.UUID{}
	if hash(pivot) == base {
		t.Fatal("nil/empty path distinction lost")
	}
	pivot.PathLinkIDs = []uuid.UUID{uuid.New(), uuid.New()}
	base = hash(pivot)
	pivot.PathLinkIDs[0], pivot.PathLinkIDs[1] = pivot.PathLinkIDs[1], pivot.PathLinkIDs[0]
	if hash(pivot) == base {
		t.Fatal("path order lost")
	}
	base = hash(pivot)
	zero := uuid.Nil
	pivot.FieldID = &zero
	if hash(pivot) == base {
		t.Fatal("nil/zero optional identity lost")
	}
	job := CreateIndexJobInput{TenantID: scope.TenantID, TableID: scope.TableID}
	base = hash(job)
	job.Columns = []string{}
	if hash(job) == base {
		t.Fatal("nil/empty columns distinction lost")
	}
	job.Columns = []string{"a", "b"}
	base = hash(job)
	job.Columns = []string{"b", "a"}
	if hash(job) == base {
		t.Fatal("column order lost")
	}
	instant := time.Date(2026, 10, 27, 12, 0, 0, 123456789, time.UTC)
	job.ScheduledAt = &instant
	base = hash(job)
	zone := instant.In(time.FixedZone("offset", 3600))
	job.ScheduledAt = &zone
	if hash(job) != base {
		t.Fatal("same instant differs by zone")
	}
	if zone.Location() == time.UTC {
		t.Fatal("caller timestamp mutated")
	}
	next := instant.Add(time.Nanosecond)
	job.ScheduledAt = &next
	if hash(job) == base {
		t.Fatal("schedule precision lost")
	}
	for _, input := range []any{
		CreateTableInput{TenantID: uuid.New()}, CreateTableInput{TenantID: scope.TenantID, SemanticType: "\xff"},
		CreateLinkInput{TenantID: scope.TenantID, ChildTable: scope.TableID},
		CreatePivotInput{TenantID: scope.TenantID, BaseTableID: uuid.New()},
		CreateIndexJobInput{TenantID: scope.TenantID, TableID: uuid.New()},
		CreateIndexJobInput{TenantID: scope.TenantID, TableID: scope.TableID, Columns: []string{"\xff"}},
	} {
		if h, err := PublicationRequestHash(scope, input); err == nil || h != ([32]byte{}) {
			t.Fatalf("invalid creation command accepted: %T", input)
		}
	}
}

func TestPublicationRequestScopeAndEnums(t *testing.T) {
	scope := requestTestScope()
	input := UpdateFieldInput{FieldID: uuid.New()}
	base, err := PublicationRequestHash(scope, input)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*PublicationRequestContext){
		func(c *PublicationRequestContext) { c.TenantID = uuid.New() },
		func(c *PublicationRequestContext) { c.TableID = uuid.New() },
		func(c *PublicationRequestContext) { c.ActorID = uuid.New() },
		func(c *PublicationRequestContext) { c.Condition.Version = uuid.New() },
		func(c *PublicationRequestContext) {
			c.Condition = contract.PublicationCondition{Mode: contract.PublicationAnyVersion}
		},
	} {
		changed := scope
		mutate(&changed)
		hash, err := PublicationRequestHash(changed, input)
		if err != nil || hash == base {
			t.Fatal("scope not bound", err)
		}
	}
	create := CreateFieldInput{TableID: scope.TableID, Name: "state", DataType: datamodel.DataTypeString, IsEnum: true}
	nilValues, err := PublicationRequestHash(scope, create)
	if err != nil {
		t.Fatal(err)
	}
	create.EnumValues = []CreateFieldEnumValueSeed{}
	emptyValues, err := PublicationRequestHash(scope, create)
	if err != nil || nilValues != emptyValues {
		t.Fatal("equivalent empty seeds differ", err)
	}
	create.EnumValues = []CreateFieldEnumValueSeed{{Value: "a", Label: "A", SortOrder: 1}, {Value: "b", Label: "B", SortOrder: 2}}
	base, err = PublicationRequestHash(scope, create)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func([]CreateFieldEnumValueSeed){
		func(v []CreateFieldEnumValueSeed) { v[0].Value = "changed" },
		func(v []CreateFieldEnumValueSeed) { v[0].Label = "changed" },
		func(v []CreateFieldEnumValueSeed) { v[0].SortOrder++ },
		func(v []CreateFieldEnumValueSeed) { v[0], v[1] = v[1], v[0] },
	} {
		changed := create
		changed.EnumValues = append([]CreateFieldEnumValueSeed(nil), create.EnumValues...)
		mutate(changed.EnumValues)
		hash, err := PublicationRequestHash(scope, changed)
		if err != nil || hash == base {
			t.Fatal("enum input lost", err)
		}
	}
	if create.EnumValues[0].Value != "a" {
		t.Fatal("caller input mutated")
	}
}

func TestPublicationRequestRejectsInvalidInputs(t *testing.T) {
	scope := requestTestScope()
	bad := string([]byte{0xff})
	for _, input := range []any{
		nil, map[string]any{"alias": "A"}, &UpdateTableInput{TableID: scope.TableID}, &CreateTableInput{TenantID: scope.TenantID},
		UpdateTableInput{TableID: uuid.New()}, CreateFieldInput{TableID: uuid.New()},
		UpdateFieldInput{}, CreateFieldEnumValueInput{}, UpdateFieldEnumValueInput{},
		UpdateTableInput{TableID: scope.TableID, Alias: &bad},
		CreateFieldInput{TableID: scope.TableID, Name: bad},
		CreateFieldInput{TableID: scope.TableID, EnumValues: []CreateFieldEnumValueSeed{{Label: bad}}},
		UpdateFieldInput{FieldID: uuid.New(), Description: &bad},
		CreateFieldEnumValueInput{FieldID: uuid.New(), Value: bad},
		UpdateFieldEnumValueInput{EnumValueID: uuid.New(), Label: &bad},
	} {
		if hash, err := PublicationRequestHash(scope, input); err == nil || hash != ([32]byte{}) {
			t.Fatalf("accepted invalid input %T", input)
		}
	}
	for _, mutate := range []func(*PublicationRequestContext){
		func(c *PublicationRequestContext) { c.TenantID = uuid.Nil },
		func(c *PublicationRequestContext) { c.TableID = uuid.Nil },
		func(c *PublicationRequestContext) { c.ActorID = uuid.Nil },
		func(c *PublicationRequestContext) { c.Condition = contract.PublicationCondition{} },
	} {
		changed := scope
		mutate(&changed)
		if _, err := PublicationRequestHash(changed, UpdateFieldInput{FieldID: uuid.New()}); err == nil {
			t.Fatal("invalid scope accepted")
		}
	}
}
