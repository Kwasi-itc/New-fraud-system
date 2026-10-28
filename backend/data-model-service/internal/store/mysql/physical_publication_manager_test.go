package mysql

import (
	"context"
	"database/sql"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/google/uuid"
)

func TestPhysicalPublicationManagerConfiguration(t *testing.T) {
	db := &sql.DB{}
	tenant, table, _ := plannerFixture()
	metadata, err := NewTransactionManager(db, "shared_app", tenant, 100, 1<<20, func(context.Context, *sql.Tx, uuid.UUID) error { return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	publication, err := NewPublicationStore(db, "shared_app", 100, 100, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	policies := []PhysicalAccessPolicy{
		{Mode: "writer", Account: installation.AccountPolicy{User: "writer", Host: "%", Objects: []installation.ObjectGrant{
			{Kind: "TABLE", Name: table.Name, Privileges: []string{"SELECT", "INSERT", "UPDATE"}},
			{Kind: "TABLE", Name: "data_model_tables", Columns: map[string][]string{"UPDATE": {"id"}}},
		}}},
		{Mode: "ddl", Account: installation.AccountPolicy{User: "ddl", Host: "%", Objects: []installation.ObjectGrant{{Kind: "TABLE", Name: table.Name, Privileges: []string{"SELECT", "CREATE", "ALTER", "INDEX"}}}}},
	}
	authorize := func(context.Context, uuid.UUID, ports.PhysicalPublicationRequest) error { return nil }
	manager, err := NewPhysicalPublicationManager(metadata, publication, table, policies, authorize)
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []func() error{
		func() error {
			_, e := NewPhysicalPublicationManager(nil, publication, table, policies, authorize)
			return e
		},
		func() error {
			_, e := NewPhysicalPublicationManager(metadata, nil, table, policies, authorize)
			return e
		},
		func() error {
			_, e := NewPhysicalPublicationManager(metadata, publication, PhysicalTable{}, policies, authorize)
			return e
		},
		func() error {
			_, e := NewPhysicalPublicationManager(metadata, publication, table, nil, authorize)
			return e
		},
		func() error {
			_, e := NewPhysicalPublicationManager(metadata, publication, table, policies, nil)
			return e
		},
		func() error {
			p := *publication
			p.db = &sql.DB{}
			_, e := NewPhysicalPublicationManager(metadata, &p, table, policies, authorize)
			return e
		},
		func() error {
			p := *publication
			p.schema = "`foreign`"
			_, e := NewPhysicalPublicationManager(metadata, &p, table, policies, authorize)
			return e
		},
	} {
		if err := invalid(); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
	policies[0].Account.Objects[0].Privileges[0] = "DELETE"
	policies[0].Account.Objects[1].Columns["UPDATE"][0] = "alias"
	policies[0].Account.Objects[1].Columns["SELECT"] = []string{"id"}
	if manager.policies[0].Account.Objects[0].Privileges[0] != "SELECT" || manager.policies[0].Account.Objects[1].Columns["UPDATE"][0] != "id" || len(manager.policies[0].Account.Objects[1].Columns) != 1 {
		t.Fatal("caller mutated policy authority")
	}
	for _, m := range []*PhysicalPublicationManager{nil, {}, manager} {
		if _, err := m.Publish(context.Background(), ports.PhysicalPublicationRequest{}, nil); err == nil {
			t.Fatal("invalid publication accepted")
		}
	}
}
