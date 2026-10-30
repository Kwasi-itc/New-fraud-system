package metadata

import (
	"reflect"
	"strings"
	"testing"
)

func TestDispatchMigrationPreservesCancellationPrefix(t *testing.T) {
	before, err := EffectCancellationStatements("fraud", "de_keys")
	if err != nil {
		t.Fatal(err)
	}
	steps, err := DispatchStatements("fraud", "de_keys")
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 149 || !reflect.DeepEqual(steps[:len(before)], before) {
		t.Fatal("rewrote migration lineage")
	}
	if _, err := DispatchPlan("fraud", "de_keys", steps); err == nil {
		t.Fatal("accepted unfingerprinted reference")
	}
	ddl := steps[145].SQL
	for _, required := range []string{"de_dispatch_identity", "FOREIGN KEY(tenant_id,kind,effect_receipt_id)", "execution_receipt_id", "ON DELETE RESTRICT", "payload_hash BINARY(32)"} {
		if !strings.Contains(ddl, required) {
			t.Fatal(required)
		}
	}
	grants, err := DispatchDefinerGrants("fraud", "de_keys")
	if err != nil || !strings.Contains(grants[len(grants)-1], "SELECT,TRIGGER") {
		t.Fatal(grants, err)
	}
}
