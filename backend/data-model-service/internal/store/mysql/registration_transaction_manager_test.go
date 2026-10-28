package mysql

import (
	"context"
	"database/sql"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/google/uuid"
)

func TestRegistrationTransactionBoundary(t *testing.T) {
	authorize := func(context.Context, *sql.Tx, uuid.UUID) error { t.Fatal("unexpected authorization"); return nil }
	if _, err := NewRegistrationTransactionManager(nil, "fixture", 100, 4096, authorize); err == nil {
		t.Fatal("nil database accepted")
	}
	if _, err := NewRegistrationTransactionManager(&sql.DB{}, "fixture", 100, 4096, nil); err == nil {
		t.Fatal("missing authorizer accepted")
	}
	manager, err := NewRegistrationTransactionManager(&sql.DB{}, "fixture", 100, 4096, authorize)
	if err != nil {
		t.Fatal(err)
	}
	callback := func(ports.MutationStore) error { t.Fatal("unexpected callback"); return nil }
	if err := manager.RunRegistration(context.Background(), uuid.Nil, callback); err == nil {
		t.Fatal("zero scope accepted")
	}
	if err := manager.RunRegistration(context.Background(), uuid.New(), nil); err == nil {
		t.Fatal("nil callback accepted")
	}
	var absent *RegistrationTransactionManager
	if err := absent.RunRegistration(context.Background(), uuid.New(), callback); err == nil {
		t.Fatal("nil manager accepted")
	}
	if err := manager.configuration.Run(context.Background(), callback); err == nil {
		t.Fatal("unbound configuration executable")
	}
}
