package mysql

import (
	"context"
	"database/sql"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/display"
	"github.com/google/uuid"
)

func TestDisplayPublicationManagerScope(t *testing.T) {
	db := &sql.DB{}
	tenant := uuid.New()
	m, err := NewTransactionManager(db, "fraud", tenant, 100, 1<<20, func(context.Context, *sql.Tx, uuid.UUID) error { return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewDisplayStore(db, "fraud", tenant, 100, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPublicationStore(db, "fraud", 100, 100, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	auth := func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error { return nil }
	if _, err := NewDisplayPublicationManager(m, s, p, nil); err == nil {
		t.Fatal("missing authorizer")
	}
	foreign := *s
	foreign.tenant = uuid.New()
	if _, err := NewDisplayPublicationManager(m, &foreign, p, auth); err == nil {
		t.Fatal("foreign tenant")
	}
	foreign = *s
	foreign.db = &sql.DB{}
	if _, err := NewDisplayPublicationManager(m, &foreign, p, auth); err == nil {
		t.Fatal("foreign pool")
	}
	other := *p
	other.schema = "`other`"
	if _, err := NewDisplayPublicationManager(m, s, &other, auth); err == nil {
		t.Fatal("foreign schema")
	}
	manager, err := NewDisplayPublicationManager(m, s, p, auth)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Publish(context.Background(), display.PublicationRequest{TenantID: tenant}, nil); err == nil {
		t.Fatal("missing effects")
	}
}
