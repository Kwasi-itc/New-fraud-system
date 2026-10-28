package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
)

type modelSnapshotStub struct {
	value ports.DataModelSnapshot
	err   error
}

func (s modelSnapshotStub) GetDataModelSnapshot(context.Context, uuid.UUID) (ports.DataModelSnapshot, error) {
	return s.value, s.err
}

func TestSnapshotDataModelReadService(t *testing.T) {
	id := uuid.New()
	value := ports.DataModelSnapshot{Tenant: tenant.Tenant{ID: id, Status: tenant.StatusActive}, Model: datamodel.AssembledDataModel{Tables: map[string]datamodel.AssembledTable{"records": {ID: uuid.New(), Name: "records"}}, Pivots: []datamodel.AssembledPivot{}}, Migrations: []datamodel.TenantSchemaMigration{{ID: uuid.New(), TenantID: id, Version: "first", AppliedAt: time.Now().UTC()}}}
	owner, err := NewSnapshotDataModelReadService(modelSnapshotStub{value: value})
	if err != nil {
		t.Fatal(err)
	}
	got, err := owner.Get(context.Background(), id)
	if err != nil || got.RevisionID != buildRevisionID(value.Tenant, value.Migrations) || got.Model.Tables["records"].ID != value.Model.Tables["records"].ID {
		t.Fatalf("owner %+v %v", got, err)
	}
	if _, err := NewSnapshotDataModelReadService(nil); err == nil {
		t.Fatal("nil snapshot accepted")
	}
	injected := errors.New("snapshot commit failure")
	owner, err = NewSnapshotDataModelReadService(modelSnapshotStub{value: value, err: injected})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := owner.Get(context.Background(), id); !errors.Is(err, injected) || got.RevisionID != "" || got.Model.Tables != nil {
		t.Fatalf("partial result %+v %v", got, err)
	}
	owner, err = NewSnapshotDataModelReadService(modelSnapshotStub{value: value})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := owner.Get(context.Background(), uuid.New()); err == nil || got.Model.Tables != nil {
		t.Fatal("foreign tenant accepted")
	}
	value.Migrations[0].TenantID = uuid.New()
	if got, err := owner.Get(context.Background(), id); err == nil || got.Model.Tables != nil {
		t.Fatal("foreign revision accepted")
	}
}

func TestBuildRevisionIDStableForEquivalentMigrationSets(t *testing.T) {
	t.Parallel()

	record := tenant.Tenant{
		ID:     uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Status: tenant.StatusActive,
	}
	appliedAtA := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	appliedAtB := time.Date(2026, 5, 20, 11, 0, 0, 0, time.UTC)

	first := []datamodel.TenantSchemaMigration{
		{
			ID:        uuid.MustParse("22222222-2222-2222-2222-222222222222"),
			Version:   "create_table:table",
			AppliedAt: appliedAtA,
		},
		{
			ID:        uuid.MustParse("33333333-3333-3333-3333-333333333333"),
			Version:   "create_field:field",
			AppliedAt: appliedAtB,
		},
	}
	second := []datamodel.TenantSchemaMigration{
		first[1],
		first[0],
	}

	firstRevision := buildRevisionID(record, first)
	secondRevision := buildRevisionID(record, second)
	if firstRevision != secondRevision {
		t.Fatalf("expected stable revision id for equivalent migration sets, got %s and %s", firstRevision, secondRevision)
	}
}

func TestBuildRevisionIDChangesWhenPublishedStateChanges(t *testing.T) {
	t.Parallel()

	baseRecord := tenant.Tenant{
		ID:     uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Status: tenant.StatusPending,
	}
	migrations := []datamodel.TenantSchemaMigration{
		{
			ID:        uuid.MustParse("22222222-2222-2222-2222-222222222222"),
			Version:   "create_tenant:tenant",
			AppliedAt: time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC),
		},
	}

	pendingRevision := buildRevisionID(baseRecord, migrations)

	activeRecord := baseRecord
	activeRecord.Status = tenant.StatusActive
	activeRevision := buildRevisionID(activeRecord, migrations)
	if pendingRevision == activeRevision {
		t.Fatal("expected revision id to change when tenant status changes")
	}

	withAdditionalMigration := append([]datamodel.TenantSchemaMigration{}, migrations...)
	withAdditionalMigration = append(withAdditionalMigration, datamodel.TenantSchemaMigration{
		ID:        uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		Version:   "create_table:table",
		AppliedAt: time.Date(2026, 5, 20, 11, 0, 0, 0, time.UTC),
	})
	changedRevision := buildRevisionID(activeRecord, withAdditionalMigration)
	if activeRevision == changedRevision {
		t.Fatal("expected revision id to change when tenant schema migrations change")
	}
}
