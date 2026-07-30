package service

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
)

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

func TestBuildPublishedRevisionIDTracksLogicalBucketLifecycle(t *testing.T) {
	t.Parallel()

	first := datamodel.LogicalBucketDefinition{
		ID:                 uuid.MustParse("44444444-4444-4444-4444-444444444444"),
		TableID:            uuid.MustParse("55555555-5555-5555-5555-555555555555"),
		TimestampFieldID:   uuid.MustParse("66666666-6666-6666-6666-666666666666"),
		TimestampFieldName: "occurred_at",
		Grain:              "daily",
		Timezone:           "Africa/Accra",
		SealDelay:          48 * time.Hour,
		DefinitionVersion:  1,
		Status:             datamodel.LogicalBucketStatusActivating,
	}
	second := first
	second.ID = uuid.MustParse("77777777-7777-7777-7777-777777777777")
	second.TimestampFieldName = "authorized_at"

	forward := buildPublishedRevisionID("base", []datamodel.LogicalBucketDefinition{first, second})
	reversed := buildPublishedRevisionID("base", []datamodel.LogicalBucketDefinition{second, first})
	if forward != reversed {
		t.Fatalf("logical bucket revision depends on repository order: %s != %s", forward, reversed)
	}

	first.Status = datamodel.LogicalBucketStatusActive
	active := buildPublishedRevisionID("base", []datamodel.LogicalBucketDefinition{first, second})
	if active == forward {
		t.Fatal("logical bucket lifecycle change did not change published revision")
	}
}
