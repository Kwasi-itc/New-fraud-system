package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var ErrDeliveryRedriveIneligible = errors.New("delivery redrive requires a discarded delivery, pending job and no reservation")
var ErrDeliveryRedriveConflict = errors.New("delivery redrive request identity already used with different inputs")

// DeliveryRedriveRequest is an internal operator request, not authorization.
// The caller must authorize this tenant and supply a trusted actor on EVERY call,
// including replay. A stable request ID and exact inputs resolve uncertain commits.
// Request identity is scoped to SourceID; each source can be redriven only once.
type DeliveryRedriveRequest struct {
	RequestID, TenantID, JobID, SourceID uuid.UUID
	Actor, Reason                        string
	ScheduledAt                          time.Time
}

type deliveryRedriveAudit struct {
	Version           int                    `json:"version"`
	Request           DeliveryRedriveRequest `json:"request"`
	Queue             string                 `json:"queue"`
	MaxAttempts       int                    `json:"max_attempts"`
	DatabasePrincipal string                 `json:"database_principal"`
	DeliveryID        uuid.UUID              `json:"delivery_id"`
}

// RedriveDiscarded atomically creates a new delivery and immutable audit record.
// It never resets domain attempts, changes domain status, deletes old evidence or
// releases another delivery's reservation. Running jobs require DDL reconciliation
// and terminal jobs require their owner retry protocol; neither is accepted here.
// Lock order is job -> source delivery -> reservation. No external calls occur.
func (s *IndexDeliveryStore) RedriveDiscarded(ctx context.Context, r DeliveryRedriveRequest) (uuid.UUID, error) {
	if err := r.Validate(); err != nil {
		return uuid.Nil, err
	}
	r.ScheduledAt = r.ScheduledAt.UTC()
	return s.redriveDiscarded(ctx, r)
}

func (r DeliveryRedriveRequest) Validate() error {
	if r.RequestID == uuid.Nil || r.TenantID == uuid.Nil || r.JobID == uuid.Nil || r.SourceID == uuid.Nil ||
		strings.TrimSpace(r.Actor) == "" || len(r.Actor) > 256 || !utf8.ValidString(r.Actor) ||
		strings.TrimSpace(r.Reason) == "" || len(r.Reason) > 2048 || !utf8.ValidString(r.Reason) || r.ScheduledAt.IsZero() {
		return errors.New("redrive identities, trusted actor, bounded reason and explicit schedule required")
	}
	if _, err := (mysqlstore.DateTime{Time: r.ScheduledAt}).Value(); err != nil {
		return err
	}
	return nil
}

func (s *IndexDeliveryStore) redriveDiscarded(ctx context.Context, r DeliveryRedriveRequest) (uuid.UUID, error) {
	var result uuid.UUID
	err := mysqlstore.Mutation(ctx, s.db, func(tx *sql.Tx) error {
		var status string
		if err := tx.QueryRowContext(ctx, "SELECT status FROM "+s.schema+".data_model_index_jobs WHERE tenant_id=? AND id=? FOR UPDATE", mysqlstore.UUID(r.TenantID), mysqlstore.UUID(r.JobID)).Scan(&status); err != nil {
			return err
		}
		var principal string
		if err := tx.QueryRowContext(ctx, "SELECT CURRENT_USER()").Scan(&principal); err != nil {
			return err
		}
		// The audit primary key is the source delivery UUID: one redrive per source,
		// even after the replacement also exhausts and releases its reservation.
		// History survives delivery retention cleanup. Bound the projection even if
		// an unrelated operation reused this UUID with a very large history payload.
		var data []byte
		var matches bool
		err := tx.QueryRowContext(ctx, "SELECT tenant_id=? AND resource_id=? AND operation='redrive_index_delivery' AND resource_type='index_job' AND status='applied',CASE WHEN OCTET_LENGTH(details)<=16384 THEN details ELSE NULL END FROM "+s.schema+".data_model_schema_change_log WHERE id=? FOR UPDATE", mysqlstore.UUID(r.TenantID), mysqlstore.UUID(r.JobID), mysqlstore.UUID(r.SourceID)).Scan(&matches, &data)
		if err == nil {
			var saved deliveryRedriveAudit
			if !matches || json.Unmarshal(data, &saved) != nil || saved.Version != 1 || saved.Request != r || saved.Queue != s.queue || saved.MaxAttempts != s.maxAttempts || saved.DatabasePrincipal != principal || saved.DeliveryID == uuid.Nil {
				return ErrDeliveryRedriveConflict
			}
			result = saved.DeliveryID
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if status != "pending" {
			return ErrDeliveryRedriveIneligible
		}
		var sourceState, sourceQueue string
		if err := tx.QueryRowContext(ctx, "SELECT state,queue_name FROM "+s.schema+".data_model_index_deliveries WHERE tenant_id=? AND job_id=? AND id=? FOR UPDATE", mysqlstore.UUID(r.TenantID), mysqlstore.UUID(r.JobID), mysqlstore.UUID(r.SourceID)).Scan(&sourceState, &sourceQueue); err != nil {
			return err
		}
		if sourceState != "discarded" || sourceQueue != s.queue {
			return ErrDeliveryRedriveIneligible
		}
		var reserved mysqlstore.UUID
		err = tx.QueryRowContext(ctx, "SELECT delivery_id FROM "+s.schema+".data_model_index_delivery_reservations WHERE tenant_id=? AND job_id=? FOR UPDATE", mysqlstore.UUID(r.TenantID), mysqlstore.UUID(r.JobID)).Scan(&reserved)
		if err == nil {
			return ErrDeliveryRedriveIneligible
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		id, err := s.EnqueueTx(ctx, tx, r.TenantID, r.JobID, &r.ScheduledAt)
		if err != nil {
			return err
		}
		audit, err := json.Marshal(deliveryRedriveAudit{Version: 1, Request: r, Queue: s.queue, MaxAttempts: s.maxAttempts, DatabasePrincipal: principal, DeliveryID: id})
		if err != nil {
			return err
		}
		if len(audit) > 16384 {
			return errors.New("redrive audit exceeds replay projection bound")
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO "+s.schema+".data_model_schema_change_log(id,tenant_id,operation,resource_type,resource_id,status,details,created_at) VALUES (?,?,'redrive_index_delivery','index_job',?,'applied',?,UTC_TIMESTAMP(6))", mysqlstore.UUID(r.SourceID), mysqlstore.UUID(r.TenantID), mysqlstore.UUID(r.JobID), audit)
		if err == nil {
			result = id
		}
		return err
	})
	if err != nil {
		return uuid.Nil, err
	}
	return result, nil
}
