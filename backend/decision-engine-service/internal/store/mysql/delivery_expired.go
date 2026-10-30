package mysql

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// ExpiredDelivery is an observed candidate, NOT authority to run/acknowledge a
// job. Rescue must reauthorize and lock/revalidate its owner, receipt and exact
// transport token/attempt/expiry. A heartbeat, another rescuer or cancellation
// may invalidate it immediately. Never run network/business work from this list.
type ExpiredDelivery struct {
	ID, Token        uuid.UUID
	Identity         mysqljobs.Identity
	TransportAttempt int
	LeaseUntil       time.Time
}

type ExpiredDeliveryCursor struct {
	LeaseUntil time.Time
	ID         uuid.UUID
}

func (s *EffectDeliveryStore) validExpired(v ExpiredDelivery) bool {
	return s != nil && s.validLease(EffectDeliveryLease{v.ID, v.Token, v.Identity, v.TransportAttempt}) && validDeliveryDate(v.LeaseUntil)
}

func validDeliveryDate(t time.Time) bool {
	u := t.UTC()
	return u.Year() >= 1000 && u.Year() <= 9999 && u.Nanosecond()%1000 == 0
}

// Expired returns a keyset page of small indexed identities, not histories or
// payloads. A fresh maintenance cycle starts with a nil cursor; a failed rescue
// must not prevent later candidates in the same bounded cycle from being tried.
// Snapshot discovery holds no row lock while the caller processes a candidate.
func (s *EffectDeliveryStore) Expired(ctx context.Context, after *ExpiredDeliveryCursor, limit int) ([]ExpiredDelivery, error) {
	if s == nil || s.db == nil || s.authorize == nil || limit < 1 || limit > 100 {
		return nil, errors.New("initialized scoped transport and expired page limit 1..100 required")
	}
	if after != nil && (after.ID == uuid.Nil || !validDeliveryDate(after.LeaseUntil)) {
		return nil, ErrStaleEffectLease
	}
	var out []ExpiredDelivery
	err := mysqlstore.Snapshot(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.authorize(ctx, tx, s.tenant, s.kind, EffectWorkerRescue); err != nil {
			return err
		}
		query, args := s.expiredSelect(after, limit)
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v := ExpiredDelivery{Identity: mysqljobs.Identity{Version: 1, TenantID: s.tenant, Kind: s.kind}}
			var until mysqlstore.DateTime
			if err := rows.Scan((*mysqlstore.UUID)(&v.ID), (*mysqlstore.UUID)(&v.Token), &v.TransportAttempt, &until, (*mysqlstore.UUID)(&v.Identity.ResourceID), &v.Identity.DomainAttempt, &v.Identity.RedriveGeneration); err != nil {
				return err
			}
			v.LeaseUntil = until.Time
			if !s.validExpired(v) {
				return ErrStaleEffectLease
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Shared with scoped EXPLAIN fixtures so plans cover the real production query,
// not a simplified surrogate. Callers validate limits/cursors before execution.
func (s *EffectDeliveryStore) expiredSelect(after *ExpiredDeliveryCursor, limit int) (string, []any) {
	query := "SELECT d.id,d.lease_token,d.attempt,d.lease_until,r.resource_id,r.domain_attempt,r.redrive_generation FROM " + s.schema + "." + s.deliveryTable() + " d JOIN " + s.schema + "." + s.receiptTable() + " r ON r.tenant_id=d.tenant_id AND r.kind=d.kind AND r.id=d.id WHERE d.tenant_id=? AND d.kind=? AND d.queue_name=? AND d.state='running' AND d.lease_until<=UTC_TIMESTAMP(6)"
	args := []any{mysqlstore.UUID(s.tenant), string(s.kind), s.queue}
	if after != nil {
		query += " AND(d.lease_until>? OR(d.lease_until=? AND d.id>?))"
		at := mysqlstore.DateTime{Time: after.LeaseUntil}
		args = append(args, at, at, mysqlstore.UUID(after.ID))
	}
	return query + " ORDER BY d.lease_until,d.id LIMIT ?", append(args, limit)
}

func (s *ExecutionDeliveryStore) Expired(ctx context.Context, after *ExpiredDeliveryCursor, limit int) ([]ExpiredDelivery, error) {
	if s == nil || s.transport == nil {
		return nil, errors.New("initialized execution transport required")
	}
	return s.transport.Expired(ctx, after, limit)
}

// The expiry equality is an observation fence; <= database time is checked
// again under lock and at final update. A renewed token/lease is never rescued.
const expiredDeliveryPredicate = " WHERE tenant_id=? AND kind=? AND queue_name=? AND id=? AND state='running' AND lease_token=? AND attempt=? AND lease_until=? AND lease_until<=UTC_TIMESTAMP(6)"

func (s *EffectDeliveryStore) expiredArgs(v ExpiredDelivery) []any {
	return []any{mysqlstore.UUID(s.tenant), string(s.kind), s.queue, mysqlstore.UUID(v.ID), mysqlstore.UUID(v.Token), v.TransportAttempt, mysqlstore.DateTime{Time: v.LeaseUntil}}
}
