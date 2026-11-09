package localdelivery

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	owner "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func (p *Provider) processScores(ctx context.Context) error {
	var pending []Delivery
	s := p.store
	if err := mysqlstore.Snapshot(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.authorize(ctx, tx, s.tenant, false); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, "SELECT resource_id,domain_attempt,redrive_generation,provider_key,payload_hash,reference,payload,result FROM "+s.schema+".decision_local_delivery_receipts WHERE tenant_id=? AND kind=? AND outcome='accepted' AND disposition='pending' ORDER BY created_at,resource_id LIMIT 20", mysqlstore.UUID(s.tenant), []byte(ports.JobScoring))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			d := Delivery{Request: ports.DispatchRequest{Kind: ports.JobScoring, TenantID: s.tenant.String()}}
			var resource, reference mysqlstore.UUID
			var hash []byte
			if err := rows.Scan(&resource, &d.Request.DomainAttempt, &d.Request.RedriveGeneration, &d.Request.Key, &hash, &reference, &d.Request.Payload, &d.Result); err != nil {
				return err
			}
			d.Request.ResourceID = uuid.UUID(resource).String()
			d.Reference = uuid.UUID(reference).String()
			d.Request.PayloadSHA256 = hex.EncodeToString(hash)
			pending = append(pending, d)
		}
		return rows.Err()
	}); err != nil {
		return err
	}
	for _, d := range pending {
		err := p.effects.ApplyScoringResult(ctx, d.Request, d.Reference, d.Result)
		disposition := "applied"
		if errors.Is(err, owner.ErrEffectJobOwnerState) || errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if errors.Is(err, owner.ErrEffectRetryState) {
			disposition = "superseded"
		} else if err != nil {
			return err
		}
		if err := mysqlstore.Mutation(ctx, s.db, func(tx *sql.Tx) error {
			if err := s.authorize(ctx, tx, s.tenant, true); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, "UPDATE "+s.schema+".decision_local_delivery_receipts SET disposition=?,applied_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND kind=? AND resource_id=? AND domain_attempt=? AND redrive_generation=? AND disposition='pending'", disposition, mysqlstore.UUID(s.tenant), []byte(d.Request.Kind), mysqlstore.UUID(uuid.MustParse(d.Request.ResourceID)), d.Request.DomainAttempt, d.Request.RedriveGeneration)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}
