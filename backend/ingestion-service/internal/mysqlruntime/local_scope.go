package mysqlruntime

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// The local operator profile may use every active published owner. Staged
// isolated runtimes retain their original tenant/table binding. Authority for
// each operation still comes from the credential and data-model SQL contract.
func (s *Service) multiTenantLocal() bool {
	return s != nil && s.Config.LocalAllTenants && s.Config.Authority == "local-mysql-development" && s.Config.Database == "fraud"
}

// IDs are globally unique resource keys, not tenant claims. This bounded lookup
// supplies routing input only; work() and Detail/Handle reauthorize the actual
// published owner and the lease transaction checks tenant/table/kind again.
func (s *Service) resourceOwner(ctx context.Context, id uuid.UUID, kind string) (uuid.UUID, string, error) {
	if !s.multiTenantLocal() {
		return s.Config.TenantID, s.Config.ObjectType, nil
	}
	if id == uuid.Nil || s.DB == nil {
		return uuid.Nil, "", service.ErrBadInput
	}
	schema, err := mysqlstore.Identifier(s.Config.Database)
	if err != nil {
		return uuid.Nil, "", err
	}
	var tenant mysqlstore.UUID
	var name mysqlstore.Text
	err = s.DB.QueryRowContext(ctx, "SELECT tenant_id,CASE WHEN OCTET_LENGTH(object_type)<=63 THEN object_type ELSE NULL END FROM "+schema+".ingestion_work_resources WHERE id=? AND kind=?", mysqlstore.UUID(id), kind).Scan(&tenant, &name)
	if err != nil {
		return uuid.Nil, "", boundary(err)
	}
	return uuid.UUID(tenant), string(name), nil
}
