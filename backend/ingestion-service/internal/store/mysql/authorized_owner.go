package mysql

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/clients/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var ErrOwnerAuthorization = errors.New("ingestion owner authorization denied")

type credentialContextKey struct{}

// WithServiceCredential carries the caller's bearer credential to the adapter.
// It confers no authority by itself: every operation and replay checks it against
// the configured service token. Do not use a caller-supplied actor/tenant header
// as proof of authorization. This preserves the existing trusted-service model.
func WithServiceCredential(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, credentialContextKey{}, token)
}

// NewAuthorizedRecordOwner binds the staged owner to actual shared metadata and
// token authentication. It cannot enable disabled-auth mode or HTTP fallback.
// The caller obtains stable table identity from an authorized route/worker lookup.
func NewAuthorizedRecordOwner(db *sql.DB, database string, tenant, table uuid.UUID, objectType, serviceToken string, limits RecordOwnerLimits, maxContractBytes int) (*KeyedRecordOwner, error) {
	if serviceToken == "" || maxContractBytes < 1 || maxContractBytes > 16<<20 {
		return nil, errors.New("token and bounded contract reader required")
	}
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	authorize := func(ctx context.Context, r ReceiptRequest) error {
		token, _ := ctx.Value(credentialContextKey{}).(string)
		if r.TenantID != tenant || r.TableID != table || token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(serviceToken)) != 1 {
			return ErrOwnerAuthorization
		}
		return nil
	}
	authorizeTx := func(ctx context.Context, tx *sql.Tx, r ReceiptRequest) error {
		if err := authorize(ctx, r); err != nil {
			return err
		}
		var status string
		var archived bool
		err := tx.QueryRowContext(ctx, "SELECT t.status,m.archived FROM "+s+".data_model_tenants t JOIN "+s+".data_model_tables m ON m.tenant_id=t.id JOIN "+s+".data_model_ingestion_contracts c ON c.tenant_id=m.tenant_id AND c.table_id=m.id WHERE t.id=? AND m.id=? AND BINARY c.table_name=?", mysqlstore.UUID(tenant), mysqlstore.UUID(table), []byte(objectType)).Scan(&status, &archived)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrOwnerAuthorization
		}
		if err != nil {
			return err
		}
		if status != "active" || archived {
			return ErrOwnerAuthorization
		}
		return nil
	}
	loader := func(ctx context.Context, tx *sql.Tx, r ReceiptRequest) (ingestion.PublishedDataModel, error) {
		var version mysqlstore.UUID
		var payload []byte
		var size int
		var status string
		err := tx.QueryRowContext(ctx, "SELECT c.version,OCTET_LENGTH(c.payload),CASE WHEN OCTET_LENGTH(c.payload)<=? THEN c.payload ELSE NULL END,t.status FROM "+s+".data_model_ingestion_contracts c JOIN "+s+".data_model_tenants t ON t.id=c.tenant_id WHERE c.tenant_id=? AND c.table_id=? AND BINARY c.table_name=?", maxContractBytes, mysqlstore.UUID(r.TenantID), mysqlstore.UUID(r.TableID), []byte(objectType)).Scan(&version, &size, &payload, &status)
		if err != nil {
			return ingestion.PublishedDataModel{}, err
		}
		if size > maxContractBytes {
			return ingestion.PublishedDataModel{}, errors.New("published contract exceeds byte bound")
		}
		definition, err := datamodel.DecodePublishedTable(payload, r.TableID, objectType)
		if err != nil {
			return ingestion.PublishedDataModel{}, err
		}
		return ingestion.PublishedDataModel{TenantID: tenant, RevisionID: fmt.Sprintf("contract-v1:%s:%s:%s", table, uuid.UUID(version), status), PublicationVersion: uuid.UUID(version), TenantStatus: status, Writable: status == "active", RecordLookupField: "object_id", PartialUpdates: true, ManagedSystemFields: []string{"object_id", "updated_at", "valid_from", "valid_until"}, Tables: map[string]ingestion.ObjectSchema{objectType: definition}}, nil
	}
	return NewKeyedRecordOwner(db, database, tenant, table, objectType, limits, authorize, authorizeTx, loader, func() time.Time { return time.Now().UTC() })
}
