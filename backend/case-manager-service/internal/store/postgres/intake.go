package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/ports"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type IntakeRepository struct{ db queryable }

func (r IntakeRepository) Lock(ctx context.Context, tenant uuid.UUID, scope, key string) error {
	value, err := json.Marshal([]string{"case-manager-intake", tenant.String(), scope, key})
	if err != nil {
		return err
	}
	digest := sha256.Sum256(value)
	_, err = r.db.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(binary.BigEndian.Uint64(digest[:8])))
	return err
}
func (r IntakeRepository) Get(ctx context.Context, tenant uuid.UUID, source string, id uuid.UUID) (*ports.IntakeReceipt, error) {
	item := ports.IntakeReceipt{TenantID: tenant, Source: source, EventID: id}
	err := r.db.QueryRow(ctx, `SELECT payload_hash,case_id,created_at FROM case_manager.intake_receipts WHERE tenant_id=$1 AND source=$2 AND event_id=$3`, tenant, source, id).Scan(&item.PayloadHash, &item.CaseID, &item.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &item, err
}
func (r IntakeRepository) Create(ctx context.Context, item ports.IntakeReceipt) error {
	_, err := r.db.Exec(ctx, `INSERT INTO case_manager.intake_receipts(tenant_id,source,event_id,payload_hash,case_id,created_at) VALUES($1,$2,$3,$4,$5,$6)`, item.TenantID, item.Source, item.EventID, item.PayloadHash, item.CaseID, item.CreatedAt)
	return err
}
