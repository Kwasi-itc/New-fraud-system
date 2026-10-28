package main

import (
	"bytes"
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"strings"
	"testing"
)

func TestAccountPoliciesDocument(t *testing.T) {
	document := accountPolicies{Format: "dm-account-policies/v1", Database: "shared_app", Accounts: []installation.AccountPolicy{{User: "operator", Host: "%", Objects: []installation.ObjectGrant{{Kind: "TABLE", Name: "records", Privileges: []string{"SELECT"}, Columns: map[string][]string{"UPDATE": {"id"}}}}}}}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if policies, e := decodeAccountPolicies(data, "shared_app"); e != nil || len(policies) != 1 || len(policies[0].Objects[0].Columns["UPDATE"]) != 1 {
		t.Fatalf("decode %v %v", policies, e)
	}
	for name, bad := range map[string][]byte{
		"wrong_database":      bytes.Replace(data, []byte("shared_app"), []byte("other_app"), 1),
		"unknown":             bytes.Replace(data, []byte(`"locked":false`), []byte(`"unknown":false`), 1),
		"duplicate":           bytes.Replace(data, []byte(`"user":"operator"`), []byte(`"user":"operator","User":"administrator"`), 1),
		"duplicate_privilege": bytes.Replace(data, []byte(`"UPDATE":["id"]`), []byte(`"UPDATE":["id"],"UPDATE":["status"]`), 1),
		"trailing":            append(append([]byte(nil), data...), data...),
		"oversize":            []byte(strings.Repeat(" ", (1<<20)+1)),
		"deep":                []byte(strings.Repeat("[", 18) + "0" + strings.Repeat("]", 18)),
		"null":                []byte("null"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeAccountPolicies(bad, "shared_app"); err == nil {
				t.Fatal("invalid policies accepted")
			}
		})
	}
	document.Accounts = append(document.Accounts, document.Accounts[0])
	data, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decodeAccountPolicies(data, "shared_app"); err == nil {
		t.Fatal("duplicate account accepted")
	}
}
