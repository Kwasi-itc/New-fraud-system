package mysql

import (
	"context"
	"database/sql"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/google/uuid"
)

func TestMetadataPublicationManagerBoundaries(t *testing.T) {
	db := &sql.DB{}
	tenant := uuid.New()
	m, err := NewTransactionManager(db, "fraud", tenant, 100, 1<<20, func(context.Context, *sql.Tx, uuid.UUID) error { return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPublicationStore(db, "fraud", 100, 100, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	auth := func(context.Context, ports.MetadataPublicationRequest) error { calls++; return nil }
	if _, err := NewMetadataPublicationManager(m, p, nil); err == nil {
		t.Fatal("missing authorizer")
	}
	wrong := *p
	wrong.db = &sql.DB{}
	if _, err := NewMetadataPublicationManager(m, &wrong, auth); err == nil {
		t.Fatal("different pool")
	}
	wrong = *p
	wrong.schema = "`other`"
	if _, err := NewMetadataPublicationManager(m, &wrong, auth); err == nil {
		t.Fatal("different database")
	}
	manager, err := NewMetadataPublicationManager(m, p, auth)
	if err != nil {
		t.Fatal(err)
	}
	base := ports.MetadataPublicationRequest{TenantID: tenant, TableID: uuid.New(), ActorID: uuid.New(), OperationID: uuid.New(), RequestHash: [32]byte{1}, Condition: contract.PublicationCondition{Mode: contract.PublicationExactVersion, Version: uuid.New()}}
	for name, change := range map[string]func(*ports.MetadataPublicationRequest){
		"foreign_tenant": func(r *ports.MetadataPublicationRequest) { r.TenantID = uuid.New() },
		"no_actor":       func(r *ports.MetadataPublicationRequest) { r.ActorID = uuid.Nil },
		"no_table":       func(r *ports.MetadataPublicationRequest) { r.TableID = uuid.Nil },
		"no_operation":   func(r *ports.MetadataPublicationRequest) { r.OperationID = uuid.Nil },
		"no_hash":        func(r *ports.MetadataPublicationRequest) { r.RequestHash = [32]byte{} },
		"any_condition": func(r *ports.MetadataPublicationRequest) {
			r.Condition = contract.PublicationCondition{Mode: contract.PublicationAnyVersion}
		},
		"absent_condition": func(r *ports.MetadataPublicationRequest) {
			r.Condition = contract.PublicationCondition{Mode: contract.PublicationMustBeAbsent}
		},
		"no_version": func(r *ports.MetadataPublicationRequest) { r.Condition.Version = uuid.Nil },
	} {
		t.Run(name, func(t *testing.T) {
			r := base
			change(&r)
			if _, err := manager.Publish(context.Background(), r, func(ports.MutationStore) error { return nil }); err == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
	if _, err := manager.Publish(context.Background(), base, nil); err == nil {
		t.Fatal("missing effects")
	}
	if calls != 0 {
		t.Fatal("invalid command reached authorization/database")
	}
}
