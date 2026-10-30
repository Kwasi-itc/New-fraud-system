package mysql

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"sort"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/clients/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/google/uuid"
)

// WithRecordRead scopes a synchronous local query to authenticated current
// metadata and the owner's publication barrier. No network calls, saved tx or
// projection, or work after the callback is permitted. It does not add routes.
// The caller must discard all callback output if this method returns an error.
func (s *IngestionContractStore) WithRecordRead(ctx context.Context, tenant uuid.UUID, name, serviceToken string, names []string, query func(context.Context, *sql.Tx, *RecordProjection) error) error {
	token, _ := ctx.Value(credentialContextKey{}).(string)
	if serviceToken == "" || token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(serviceToken)) != 1 {
		return ErrOwnerAuthorization
	}
	if s == nil || query == nil {
		return errors.New("local read callback required")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	state, err := s.read(ctx, tx, tenant, name, uuid.Nil, uuid.Nil)
	if err != nil {
		return err
	}
	if state.TableID == uuid.Nil || state.TenantStatus != "active" {
		return ErrOwnerAuthorization
	}
	table, err := datamodel.DecodePublishedTable(state.Payload, state.TableID, name)
	if err != nil {
		return err
	}
	if names == nil {
		names = make([]string, 0, len(table.Fields))
		for name, field := range table.Fields {
			if !field.Archived {
				names = append(names, name)
			}
		}
		sort.Strings(names)
	}
	model := ingestion.PublishedDataModel{TenantID: tenant, Writable: true, PublicationVersion: state.Version, RecordLookupField: "object_id", Tables: map[string]ingestion.ObjectSchema{name: table}}
	projection, err := NewRecordProjection(ctx, tx, s.schemaName(), tenant, model, name, state.Version, names)
	if err != nil {
		return err
	}
	// Lock acquisition can wait behind publication. Recheck the contract after
	// acquiring it, so renamed/recreated/revoked metadata cannot reuse old mappings.
	current, err := s.read(ctx, tx, tenant, name, state.TableID, state.Version)
	if err != nil {
		return err
	}
	if current.TableID != state.TableID || current.Version != state.Version || current.TenantStatus != "active" {
		return ErrWriteUnavailable
	}
	if err = query(ctx, tx, projection); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *IngestionContractStore) schemaName() string { return s.schema[1 : len(s.schema)-1] }
