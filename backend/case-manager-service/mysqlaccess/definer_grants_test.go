package mysqlaccess

import (
	"strings"
	"testing"
)

func TestDeclaredCaseFunctionsAreExecutableByTheirLockedDefiner(t *testing.T) {
	steps, err := Statements("case_test", "case_guard")
	if err != nil {
		t.Fatal(err)
	}
	grants, err := DefinerGrants("case_test", "case_guard")
	if err != nil {
		t.Fatal(err)
	}
	rendered := strings.Join(grants, "\n")
	for _, step := range steps {
		if step.Kind == "FUNCTION" && !strings.Contains(rendered, "GRANT EXECUTE ON FUNCTION `case_test`."+step.Name+" TO `case_guard`@`localhost`") {
			t.Fatalf("definer cannot execute its own function %s", step.Name)
		}
	}
}
