package screening

import (
	"encoding/json"
	"github.com/google/uuid"
	"reflect"
	"testing"
)

func TestAuthoringValidation(t *testing.T) {
	valid := AuthoringRule{ID: uuid.NewString(), InboxID: uuid.NewString(), Name: "Review", EntityType: "Person", NameField: "full_name", Outcomes: []string{"review"}}
	for _, change := range []func(*AuthoringRule){func(r *AuthoringRule) { r.EntityType = "Unknown" }, func(r *AuthoringRule) { r.Outcomes = []string{"review", "review"} }, func(r *AuthoringRule) { r.Outcomes = []string{"unknown"} }, func(r *AuthoringRule) { r.NameField = "name;drop" }, func(r *AuthoringRule) { r.InboxID = "" }} {
		r := valid
		change(&r)
		if r.Validate() == nil {
			t.Fatal("invalid rule accepted", r)
		}
	}
	for _, entity := range []string{"Person", "Organization"} {
		for _, address := range []string{"", "postal_address"} {
			r := valid
			r.EntityType = entity
			r.AddressField = address
			c := Config{ID: r.ID, Name: r.Name, Provider: "opensanctions", ConfigJSON: r.ConfigJSON(), AllowedOutcomes: r.Outcomes, ReviewInboxID: &r.InboxID}
			if _, ok := DecodeAuthoring(c); !ok {
				t.Fatal("valid contract rejected")
			}
			c.ConfigJSON = json.RawMessage(`{"authoring_version":1,"entity_type":"Person","query_fields":{"name":"full_name"},"unknown_setting":2}`)
			if _, ok := DecodeAuthoring(c); ok {
				t.Fatal("unsupported settings silently dropped")
			}
		}
	}
}

func TestCombinedNameAuthoringRoundTrip(t *testing.T) {
	r := AuthoringRule{ID: uuid.NewString(), InboxID: uuid.NewString(), Name: "Combined name", EntityType: "Person", NameField: "first", AdditionalNameFields: []string{"middle", "last"}, Outcomes: []string{"review"}}
	decoded, ok := DecodeAuthoring(Config{ID: r.ID, Name: r.Name, Provider: "opensanctions", ConfigJSON: r.ConfigJSON(), AllowedOutcomes: r.Outcomes, ReviewInboxID: &r.InboxID})
	if !ok || !reflect.DeepEqual(decoded.AdditionalNameFields, r.AdditionalNameFields) {
		t.Fatal("ordered mapping did not round trip", decoded, ok)
	}
	for _, fields := range [][]string{{"first"}, {"last", "last"}, {"bad;name"}, {""}, {"a", "b", "c", "d", "e"}} {
		r.AdditionalNameFields = fields
		if r.Validate() == nil {
			t.Fatal("invalid combined mapping accepted", fields)
		}
	}
}

func TestBirthDateAuthoringContract(t *testing.T) {
	r := AuthoringRule{ID: uuid.NewString(), InboxID: uuid.NewString(), Name: "DOB", EntityType: "Person", NameField: "name", BirthDateField: "dob", Outcomes: []string{"approve"}}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	got, ok := DecodeAuthoring(Config{ID: r.ID, Name: r.Name, Provider: "opensanctions", ConfigJSON: r.ConfigJSON(), AllowedOutcomes: r.Outcomes, ReviewInboxID: &r.InboxID})
	if !ok || got.BirthDateField != "dob" {
		t.Fatal(got, ok)
	}
	r.EntityType = "Organization"
	if r.Validate() == nil {
		t.Fatal("Organization accepted DOB")
	}
	r.EntityType = "Person"
	r.BirthDateField = "bad;field"
	if r.Validate() == nil {
		t.Fatal("invalid mapping accepted")
	}
}

func TestCountryAuthoringContract(t *testing.T) {
	for _, entity := range []string{"Person", "Organization"} {
		r := AuthoringRule{ID: uuid.NewString(), InboxID: uuid.NewString(), Name: "Country identity", EntityType: entity, NameField: "name", Outcomes: []string{"approve"}}
		if entity == "Person" {
			r.NationalityField = "code"
		} else {
			r.CountryField = "code"
		}
		if err := r.Validate(); err != nil {
			t.Fatal(err)
		}
		got, ok := DecodeAuthoring(Config{ID: r.ID, Name: r.Name, Provider: "opensanctions", ConfigJSON: r.ConfigJSON(), AllowedOutcomes: r.Outcomes, ReviewInboxID: &r.InboxID})
		if !ok || got.NationalityField != r.NationalityField || got.CountryField != r.CountryField {
			t.Fatal(got, ok)
		}
		if entity == "Person" {
			r.EntityType = "Organization"
		} else {
			r.EntityType = "Person"
		}
		if r.Validate() == nil {
			t.Fatal("incompatible entity accepted")
		}
		r.EntityType = entity
		if entity == "Person" {
			r.NationalityField = "invalid;name"
		} else {
			r.CountryField = "invalid;name"
		}
		if r.Validate() == nil {
			t.Fatal("invalid field accepted")
		}
	}
}

func TestRegistrationAuthoringContract(t *testing.T) {
	r := AuthoringRule{ID: uuid.NewString(), InboxID: uuid.NewString(), Name: "Registration", EntityType: "Organization", NameField: "name", RegistrationField: "registration", Outcomes: []string{"approve"}}
	got, ok := DecodeAuthoring(Config{ID: r.ID, Name: r.Name, Provider: "opensanctions", ConfigJSON: r.ConfigJSON(), AllowedOutcomes: r.Outcomes, ReviewInboxID: &r.InboxID})
	if !ok || got.RegistrationField != r.RegistrationField {
		t.Fatal(got, ok)
	}
	r.EntityType = "Person"
	if r.Validate() == nil {
		t.Fatal("Person registration accepted")
	}
	r.EntityType = "Organization"
	r.RegistrationField = "bad;name"
	if r.Validate() == nil {
		t.Fatal("invalid field accepted")
	}
}
