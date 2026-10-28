package service

import (
	"crypto/sha256"
	"reflect"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/google/uuid"
)

func registrationRequestScope() RegistrationRequestContext {
	return RegistrationRequestContext{TenantID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), ActorID: uuid.MustParse("22222222-2222-2222-2222-222222222222")}
}

func TestRegistrationRequestGolden(t *testing.T) {
	key := " External "
	input := tenant.CreateInput{Name: " Tenant ", ExternalKey: &key}
	want := `{"format":"dm-tenant-registration/v1","tenant_id":"11111111-1111-1111-1111-111111111111","actor_id":"22222222-2222-2222-2222-222222222222","kind":"tenant.create","body":{"name":" Tenant ","external_key":" External "}}`
	encoded, err := encodeRegistrationRequest(registrationRequestScope(), input)
	if err != nil || string(encoded) != want {
		t.Fatalf("registration format changed: %s %v", encoded, err)
	}
	hash, err := RegistrationRequestHash(registrationRequestScope(), input)
	if err != nil || hash != sha256.Sum256([]byte(want)) {
		t.Fatalf("fingerprint differs: %v", err)
	}
	if input.Name != " Tenant " || key != " External " {
		t.Fatal("request mutated")
	}
}

func TestRegistrationRequestEveryField(t *testing.T) {
	scope := registrationRequestScope()
	input := tenant.CreateInput{Name: "Tenant"}
	base, err := RegistrationRequestHash(scope, input)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < reflect.TypeOf(input).NumField(); i++ {
		t.Run(reflect.TypeOf(input).Field(i).Name, func(t *testing.T) {
			changed := input
			field := reflect.ValueOf(&changed).Elem().Field(i)
			switch field.Kind() {
			case reflect.String:
				field.SetString("changed")
			case reflect.Pointer:
				if field.Type() != reflect.TypeOf((*string)(nil)) {
					t.Fatal("new input field requires encoding coverage")
				}
				field.Set(reflect.ValueOf(new(string)))
			default:
				t.Fatal("new input field requires encoding coverage")
			}
			if hash, err := RegistrationRequestHash(scope, changed); err != nil || hash == base {
				t.Fatalf("input field omitted: %v", err)
			}
		})
	}
	for _, changed := range []RegistrationRequestContext{{uuid.New(), scope.ActorID}, {scope.TenantID, uuid.New()}} {
		if hash, err := RegistrationRequestHash(changed, input); err != nil || hash == base {
			t.Fatalf("identity omitted: %v", err)
		}
	}
}

func TestRegistrationRequestOriginalInputDistinctions(t *testing.T) {
	scope := registrationRequestScope()
	hashes := map[[32]byte]bool{}
	for _, input := range []tenant.CreateInput{
		{Name: "Tenant"}, {Name: " Tenant "}, {Name: "tenant"},
		{Name: "Tenant", ExternalKey: registrationString("")},
		{Name: "Tenant", ExternalKey: registrationString(" ")},
		{Name: "Tenant", ExternalKey: registrationString("Key")},
		{Name: "Tenant", ExternalKey: registrationString(" Key ")},
		{Name: "Tenant", ExternalKey: registrationString("key")},
		{Name: "\u00e9"}, {Name: "e\u0301"},
	} {
		hash, err := RegistrationRequestHash(scope, input)
		if err != nil || hashes[hash] {
			t.Fatalf("request distinction lost: %+v %v", input, err)
		}
		hashes[hash] = true
	}
	// Model normalization deliberately collapses some of those inputs; receipt
	// fingerprints must still distinguish the original commands.
	a := tenant.New(scope.TenantID, registrationBoundaryClock{}.Now(), "Tenant", nil)
	b := tenant.New(scope.TenantID, a.CreatedAt, " Tenant ", registrationString(" "))
	if !reflect.DeepEqual(a, b) {
		t.Fatal("normalization characterization changed")
	}
}

func TestRegistrationRequestRejectsInvalidIdentityAndEncoding(t *testing.T) {
	scope := registrationRequestScope()
	for _, input := range []tenant.CreateInput{{Name: "\xff"}, {Name: "Tenant", ExternalKey: registrationString("\xff")}} {
		if hash, err := RegistrationRequestHash(scope, input); err == nil || hash != ([32]byte{}) {
			t.Fatal("invalid UTF-8 accepted")
		}
	}
	for _, bad := range []RegistrationRequestContext{{}, {TenantID: scope.TenantID}, {ActorID: scope.ActorID}} {
		if hash, err := RegistrationRequestHash(bad, tenant.CreateInput{Name: "Tenant"}); err == nil || hash != ([32]byte{}) {
			t.Fatal("invalid identity accepted")
		}
	}
}

func registrationString(value string) *string { return &value }
