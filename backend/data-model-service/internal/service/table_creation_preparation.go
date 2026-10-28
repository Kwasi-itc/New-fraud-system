package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/google/uuid"
)

// PrepareCreation allocates and retains the original owner result with its two
// mandatory fields and closed registry. It is not Create success. The caller
// must retain operationID, bind/execute the complete reference plan, and publish
// using PublishPreparedCreation before RecoverCreation can return success.
// Manager authorization binds the actual actor on every invocation and replay.
func (s TableService) PrepareCreation(ctx context.Context, input CreateTableInput, operationID, actorID uuid.UUID, manager ports.TableCreationManager) (ports.TableCreationAllocation, error) {
	if manager == nil || s.idGenerator == nil || s.clock == nil || operationID == uuid.Nil {
		return ports.TableCreationAllocation{}, errors.New("creation manager, owner dependencies and operation required")
	}
	if err := datamodel.ValidateTableCreate(input.Name); err != nil {
		return ports.TableCreationAllocation{}, err
	}
	if err := datamodel.ValidateSemanticType(input.SemanticType); err != nil {
		return ports.TableCreationAllocation{}, err
	}
	hash, err := TableCreationRequestHash(actorID, input)
	if err != nil {
		return ports.TableCreationAllocation{}, err
	}
	return manager.Allocate(ctx, ports.TableCreationRequest{TenantID: input.TenantID, OperationID: operationID, ActorID: actorID, RequestHash: hash}, func() (ports.TableCreationAllocation, error) {
		at := s.clock.Now().UTC().Truncate(time.Microsecond)
		table := datamodel.Table{ID: s.idGenerator.New(), TenantID: input.TenantID, Name: datamodel.NormalizeName(input.Name), Description: input.Description, Alias: input.Alias, SemanticType: datamodel.NormalizeName(input.SemanticType), CreatedAt: at, UpdatedAt: at}
		fields := [2]datamodel.Field{
			{ID: s.idGenerator.New(), TenantID: input.TenantID, TableID: table.ID, Name: "object_id", Description: fmt.Sprintf("required id on all objects in the %s table", table.Name), DataType: datamodel.DataTypeString, IsUnique: true, CreatedAt: at, UpdatedAt: at},
			{ID: s.idGenerator.New(), TenantID: input.TenantID, TableID: table.ID, Name: "updated_at", Description: fmt.Sprintf("required timestamp on all objects in the %s table", table.Name), DataType: datamodel.DataTypeTimestamp, CreatedAt: at, UpdatedAt: at},
		}
		publicationHash, err := PublicationRequestHash(PublicationRequestContext{TenantID: input.TenantID, TableID: table.ID, ActorID: actorID, Condition: contract.PublicationCondition{Mode: contract.PublicationMustBeAbsent}}, input)
		if err != nil {
			return ports.TableCreationAllocation{}, err
		}
		return ports.TableCreationAllocation{Snapshot: ports.TableCreationSnapshot{Format: "dm-table-creation-result/v1", Table: table, Fields: fields}, PublicationHash: publicationHash}, nil
	})
}

// RecoverCreation returns immutable original owner output after the complete
// physical publication commits, including after a lost commit acknowledgement.
// Current table metadata is never substituted for a historical result.
func (s TableService) RecoverCreation(ctx context.Context, input CreateTableInput, operationID, actorID uuid.UUID, manager ports.TableCreationManager) (datamodel.Table, error) {
	if manager == nil || operationID == uuid.Nil {
		return datamodel.Table{}, errors.New("creation manager and operation required")
	}
	hash, err := TableCreationRequestHash(actorID, input)
	if err != nil {
		return datamodel.Table{}, err
	}
	return manager.Recover(ctx, ports.TableCreationRequest{TenantID: input.TenantID, OperationID: operationID, ActorID: actorID, RequestHash: hash})
}
