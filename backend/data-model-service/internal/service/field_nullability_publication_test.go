package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/google/uuid"
)

type fieldNullabilityPublisher struct{ store ports.MutationStore }

func (p fieldNullabilityPublisher) Publish(_ context.Context, _ ports.PhysicalPublicationRequest, effects func(ports.MutationStore) error) (contract.Publication, error) {
	return contract.Publication{}, effects(p.store)
}
func TestFieldNullabilityOwnerScope(t *testing.T) {
	for _, scenario := range []string{"inactive_tenant", "foreign_table", "archived_table", "foreign_field_tenant", "foreign_field_table", "archived_field", "missing_field", "reserved_field", "enum_field", "unique_input", "enum_input", "missing_nullable"} {
		t.Run(scenario, func(t *testing.T) {
			tenantID, tableID, fieldID := uuid.New(), uuid.New(), uuid.New()
			tenantRecord := tenant.Tenant{ID: tenantID, Status: tenant.StatusActive}
			table := datamodel.Table{ID: tableID, TenantID: tenantID}
			fields := &presentationFieldRepository{field: datamodel.Field{ID: fieldID, TenantID: tenantID, TableID: tableID}}
			switch scenario {
			case "reserved_field":
				fields.field.Name = "object_id"
			case "enum_field":
				fields.field.IsEnum = true
			case "inactive_tenant":
				tenantRecord.Status = tenant.Status("pending")
			case "foreign_table":
				table.TenantID = uuid.New()
			case "archived_table":
				table.Archived = true
			case "foreign_field_tenant":
				fields.field.TenantID = uuid.New()
			case "foreign_field_table":
				fields.field.TableID = uuid.New()
			case "archived_field":
				fields.field.Archived = true
			case "missing_field":
				fields.err = errors.New("not found")
			}
			store := stubMutationStore{tenants: stubTenantRepository{record: tenantRecord}, tables: stubTableRepository{table: table}, fields: fields}
			owner := NewFieldService(nil, nil, nil, nil, nil, nil, nil, nil, nil, stubIDGenerator{}, stubClock{})
			alias := "alias"
			flag := false
			input := UpdateFieldInput{FieldID: fieldID, Alias: &alias, Nullable: &flag}
			if scenario == "unique_input" {
				input.IsUnique = &flag
			}
			if scenario == "enum_input" {
				input.IsEnum = &flag
			}
			if scenario == "missing_nullable" {
				input.Nullable = nil
			}
			actor := uuid.New()
			r := ports.PhysicalPublicationRequest{TableID: tableID, ActorKind: "api", ActorID: actor.String(), OperationID: uuid.New(), Condition: contract.PublicationCondition{Mode: contract.PublicationExactVersion, Version: uuid.New()}}
			var err error
			r.RequestHash, err = PublicationRequestHash(PublicationRequestContext{TenantID: tenantID, TableID: tableID, ActorID: actor, Condition: r.Condition}, input)
			if err != nil {
				t.Fatal(err)
			}
			if p, err := owner.PublishPreparedNullabilityUpdate(context.Background(), tenantID, input, r, fieldNullabilityPublisher{store}); err == nil || p.Version != uuid.Nil || fields.updated {
				t.Fatal("invalid owner scope reached write", err, fields.updated)
			}
		})
	}
}
