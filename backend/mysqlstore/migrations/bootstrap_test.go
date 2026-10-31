package migrations

import (
	"context"
	"reflect"
	"testing"
)

func TestBootstrapGraph(t *testing.T) {
	noop := func(context.Context) error { return nil }
	pass := func(name string, deps ...string) BootstrapPass {
		return BootstrapPass{Name: name, Database: "shared", DependsOn: deps, Apply: noop, Verify: noop}
	}
	order, err := bootstrapOrder("shared", []BootstrapPass{pass("grants", "routines"), pass("routines", "tables"), pass("tables")})
	if err != nil || !reflect.DeepEqual(order, []int{2, 1, 0}) {
		t.Fatalf("order %v %v", order, err)
	}
	for _, passes := range [][]BootstrapPass{nil, {pass("one", "missing")}, {pass("one"), pass("one")}, {pass("one", "two"), pass("two", "one")}, {pass("one", "one")}, {pass("one", "two", "two"), pass("two")}, {{Name: "one", Database: "other", Apply: noop, Verify: noop}}, {{Name: "one", Database: "shared", Apply: noop}}} {
		if _, err := bootstrapOrder("shared", passes); err == nil {
			t.Fatal("invalid graph accepted")
		}
	}
}
