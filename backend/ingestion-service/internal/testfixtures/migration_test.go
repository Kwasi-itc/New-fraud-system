package testfixtures

import (
	"github.com/google/uuid"
	"testing"
)

func TestSourceFixtureCopies(t *testing.T) {
	a, b := MigrationRecord().Payload(), MigrationRecord().Payload()
	a["amount"] = 0
	if b["amount"] != float64(10.5) || string(MigrationRecord().CSV()) != "object_id,amount,status\nrecord-1,10.5,pending\n" {
		t.Fatal(b)
	}
	first, second := SourceTransactionModel(uuid.New()), SourceTransactionModel(uuid.New())
	first.Tables["transactions"].Fields["status"].EnumValues[0].Value = "changed"
	if second.Tables["transactions"].Fields["status"].EnumValues[0].Value != "pending" {
		t.Fatal("shared mutable source fixture")
	}
}
