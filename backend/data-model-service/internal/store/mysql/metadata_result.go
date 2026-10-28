package mysql

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/google/uuid"
)

const tableResultKind = "datamodel.table/v1"
const fieldResultKind = "datamodel.field/v1"

type metadataResult struct {
	kind  string
	id    uuid.UUID
	raw   []byte
	table datamodel.Table
	field datamodel.Field
}

func (m *MetadataPublicationManager) PublishTable(ctx context.Context, r ports.MetadataPublicationRequest, effects func(ports.MutationStore) error) (datamodel.Table, error) {
	retained := &metadataResult{kind: tableResultKind, id: r.TableID}
	if _, err := m.publish(ctx, r, effects, retained); err != nil {
		return datamodel.Table{}, err
	}
	return retained.table, nil
}
func (m *MetadataPublicationManager) PublishField(ctx context.Context, r ports.MetadataPublicationRequest, field uuid.UUID, effects func(ports.MutationStore) error) (datamodel.Field, error) {
	if field == uuid.Nil {
		return datamodel.Field{}, errors.New("original field identity required")
	}
	retained := &metadataResult{kind: fieldResultKind, id: field}
	if _, err := m.publish(ctx, r, effects, retained); err != nil {
		return datamodel.Field{}, err
	}
	return retained.field, nil
}

func (r *metadataResult) capture(ctx context.Context, repos MetadataRepositories, table uuid.UUID) error {
	var err error
	switch r.kind {
	case tableResultKind:
		r.table, err = repos.Tables.GetByID(ctx, table)
		if err == nil {
			r.raw, err = json.Marshal(r.table)
		}
	case fieldResultKind:
		r.field, err = repos.Fields.GetByID(ctx, r.id)
		if err == nil {
			r.raw, err = json.Marshal(r.field)
		}
	default:
		return errors.New("unsupported owner result kind")
	}
	return err
}
func (r *metadataResult) decode(tenant, table uuid.UUID) error {
	switch r.kind {
	case tableResultKind:
		if err := json.Unmarshal(r.raw, &r.table); err != nil {
			return err
		}
		if r.table.ID != table || r.id != table || r.table.TenantID != tenant {
			return ErrPublicationOperationConflict
		}
	case fieldResultKind:
		if err := json.Unmarshal(r.raw, &r.field); err != nil {
			return err
		}
		if r.field.ID != r.id || r.field.TableID != table || r.field.TenantID != tenant {
			return ErrPublicationOperationConflict
		}
	default:
		return errors.New("unsupported owner result kind")
	}
	return nil
}

var _ ports.MetadataResultPublicationManager = (*MetadataPublicationManager)(nil)
