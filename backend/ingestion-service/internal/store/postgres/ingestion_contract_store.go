package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type IngestionContractStore struct{ db txExecutor }

func NewIngestionContractStore(db txExecutor) IngestionContractStore {
	return IngestionContractStore{db: db}
}

// Check the versioned read contract at startup, before accepting traffic/jobs.
func (s IngestionContractStore) Validate(ctx context.Context) error {
	rows, err := s.db.Query(ctx, `SELECT tenant_id, table_id, table_name, version::uuid, payload, tenant_status, schema_name FROM core.ingestion_contracts_v1 WHERE false`)
	if err != nil {
		return fmt.Errorf("data-model migration 000005 is required for SQL ingestion contracts: %w", err)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("validate ingestion table contract: %w", err)
	}
	rows, err = s.db.Query(ctx, `SELECT tenant_id, tenant_status, schema_name FROM core.ingestion_tenants_v1 WHERE false`)
	if err != nil {
		return fmt.Errorf("validate ingestion tenant contract: %w", err)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("validate ingestion tenant contract: %w", err)
	}
	return nil
}

func (s IngestionContractStore) ReadContract(ctx context.Context, tenantID uuid.UUID, objectType string, knownTable uuid.UUID, knownVersion uuid.UUID) (ports.PublishedContract, error) {
	state := ports.PublishedContract{TenantID: tenantID}
	// Status, version and payload are read in one statement/snapshot. CASE avoids
	// transferring the stored JSON on a hit. All identifiers are fixed; values bind.
	err := s.db.QueryRow(ctx, `SELECT table_id, table_name, schema_name, tenant_status, version,
		CASE WHEN table_id = $3 AND version = $4 THEN NULL ELSE payload END
		FROM core.ingestion_contracts_v1 WHERE tenant_id = $1 AND table_name = $2`,
		tenantID, objectType, knownTable, knownVersion).Scan(&state.TableID, &state.TableName, &state.SchemaName, &state.TenantStatus, &state.Version, &state.Payload)
	if errors.Is(err, pgx.ErrNoRows) {
		// Preserve validation of unknown object types for existing tenants. This
		// exceptional path does not cache an absence or a tenant's status.
		err = s.db.QueryRow(ctx, `SELECT schema_name, tenant_status FROM core.ingestion_tenants_v1 WHERE tenant_id = $1`, tenantID).Scan(&state.SchemaName, &state.TenantStatus)
	}
	if err != nil {
		return ports.PublishedContract{}, fmt.Errorf("read published ingestion contract: %w", err)
	}
	return state, nil
}
