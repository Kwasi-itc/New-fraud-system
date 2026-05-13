package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
)

type stubPivotRepository struct {
	pivot     datamodel.Pivot
	getErr    error
	deleteErr error
	deletedID uuid.UUID
}

func (s *stubPivotRepository) Create(context.Context, datamodel.Pivot) error { return nil }
func (s *stubPivotRepository) GetByID(context.Context, uuid.UUID) (datamodel.Pivot, error) {
	return s.pivot, s.getErr
}
func (s *stubPivotRepository) ListByTenant(context.Context, uuid.UUID) ([]datamodel.Pivot, error) {
	return nil, nil
}
func (s *stubPivotRepository) Delete(_ context.Context, id uuid.UUID) error {
	s.deletedID = id
	return s.deleteErr
}

type stubSchemaChangeRepository struct {
	changes []datamodel.SchemaChange
}

func (s *stubSchemaChangeRepository) Create(_ context.Context, change datamodel.SchemaChange) error {
	s.changes = append(s.changes, change)
	return nil
}
func (s *stubSchemaChangeRepository) ListByTenant(context.Context, uuid.UUID) ([]datamodel.SchemaChange, error) {
	return nil, nil
}

type stubIDGenerator struct {
	value uuid.UUID
}

func (s stubIDGenerator) New() uuid.UUID { return s.value }

type stubClock struct {
	now time.Time
}

func (s stubClock) Now() time.Time { return s.now }

func TestPivotServiceDeleteDryRunDoesNotDelete(t *testing.T) {
	t.Parallel()

	pivot := datamodel.Pivot{ID: uuid.New(), TenantID: uuid.New()}
	pivotRepo := &stubPivotRepository{pivot: pivot}
	changeRepo := &stubSchemaChangeRepository{}
	service := NewPivotService(
		nil,
		nil,
		nil,
		pivotRepo,
		changeRepo,
		stubTransactionManager{store: stubMutationStore{pivots: pivotRepo, schemaChanges: changeRepo}},
		stubIDGenerator{value: uuid.New()},
		stubClock{now: time.Now().UTC()},
	)

	report, err := service.Delete(context.Background(), pivot.ID, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Performed {
		t.Fatal("expected dry run not to perform delete")
	}
	if pivotRepo.deletedID != uuid.Nil {
		t.Fatal("expected no delete call during dry run")
	}
	if len(changeRepo.changes) != 0 {
		t.Fatal("expected no schema changes recorded during dry run")
	}
}

func TestPivotServiceDeleteDeletesAndLogs(t *testing.T) {
	t.Parallel()

	pivot := datamodel.Pivot{ID: uuid.New(), TenantID: uuid.New()}
	now := time.Now().UTC()
	pivotRepo := &stubPivotRepository{pivot: pivot}
	changeRepo := &stubSchemaChangeRepository{}
	service := NewPivotService(
		nil,
		nil,
		nil,
		pivotRepo,
		changeRepo,
		stubTransactionManager{store: stubMutationStore{pivots: pivotRepo, schemaChanges: changeRepo}},
		stubIDGenerator{value: uuid.New()},
		stubClock{now: now},
	)

	report, err := service.Delete(context.Background(), pivot.ID, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !report.Performed {
		t.Fatal("expected delete to be marked performed")
	}
	if pivotRepo.deletedID != pivot.ID {
		t.Fatal("expected pivot repository delete call")
	}
	if len(changeRepo.changes) != 1 {
		t.Fatalf("expected 1 schema change, got %d", len(changeRepo.changes))
	}
	if changeRepo.changes[0].ResourceID != pivot.ID {
		t.Fatal("expected schema change to reference deleted pivot")
	}
}
