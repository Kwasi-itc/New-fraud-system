package mysql

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var ErrStaleDeliveryLease = errors.New("index delivery lease is stale or expired")
var ErrDeliveryTerminal = errors.New("index delivery is already terminal")
var queueNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)
var deliveryErrorPattern = regexp.MustCompile(`^[a-z0-9_]{0,64}$`)

// DeliveryLease is not authorization. Only the internal worker receives tokens;
// tenant/job identity must be checked again before every local domain outcome.
type DeliveryLease struct {
	ID, TenantID, JobID, Token uuid.UUID
	Attempt                    int
}

type IndexDeliveryStore struct {
	db            *sql.DB
	schema, queue string
	maxAttempts   int
	lease         time.Duration
	retryPolicy   ports.DeliveryRetryPolicy
}

func NewIndexDeliveryStore(db *sql.DB, database, queue string, maxAttempts int, lease time.Duration) (*IndexDeliveryStore, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if db == nil || !queueNamePattern.MatchString(queue) || maxAttempts < 1 || maxAttempts > 2147483647 || lease < time.Millisecond || lease > time.Hour || lease%time.Microsecond != 0 {
		return nil, errors.New("database, queue, attempts and microsecond lease in 1ms..1h required")
	}
	return &IndexDeliveryStore{db: db, schema: schema, queue: queue, maxAttempts: maxAttempts, lease: lease}, nil
}

// NewIndexDeliveryStoreWithRetryPolicy requires the v002 error-count upgrade.
// It never installs schema or infers historical errors from delivery attempts.
func NewIndexDeliveryStoreWithRetryPolicy(db *sql.DB, database, queue string, maxAttempts int, lease time.Duration, policy ports.DeliveryRetryPolicy) (*IndexDeliveryStore, error) {
	if policy == nil {
		return nil, errors.New("delivery retry policy required")
	}
	s, err := NewIndexDeliveryStore(db, database, queue, maxAttempts, lease)
	if err != nil {
		return nil, err
	}
	s.retryPolicy = policy
	return s, nil
}

// EnqueueTx participates in the owner's metadata/history transaction. The exact
// job row serializes its reservation across queues, matching River ByArgs without
// ByQueue. A retained completed delivery also deduplicates until cleanup.
func (s *IndexDeliveryStore) EnqueueTx(ctx context.Context, tx *sql.Tx, tenant, job uuid.UUID, at *time.Time) (uuid.UUID, error) {
	if tx == nil || tenant == uuid.Nil || job == uuid.Nil {
		return uuid.Nil, errors.New("transaction and tenant/job required")
	}
	if at != nil {
		if _, err := (mysqlstore.DateTime{Time: *at}).Value(); err != nil {
			return uuid.Nil, err
		}
	}
	if err := s.lockJob(ctx, tx, tenant, job, false); err != nil {
		return uuid.Nil, err
	}
	var existing mysqlstore.UUID
	err := tx.QueryRowContext(ctx, "SELECT delivery_id FROM "+s.schema+".data_model_index_delivery_reservations WHERE tenant_id=? AND job_id=? FOR UPDATE", mysqlstore.UUID(tenant), mysqlstore.UUID(job)).Scan(&existing)
	if err == nil {
		return uuid.UUID(existing), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, err
	}
	id := uuid.New()
	columns, values := "", ""
	if s.retryPolicy != nil {
		columns = ",error_count"
		values = ",0"
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO "+s.schema+".data_model_index_deliveries(id,tenant_id,job_id,queue_name,state,available_at,created_at,max_attempts"+columns+") VALUES (?,?,?,?,'available',COALESCE(?,UTC_TIMESTAMP(6)),UTC_TIMESTAMP(6),?"+values+")", mysqlstore.UUID(id), mysqlstore.UUID(tenant), mysqlstore.UUID(job), s.queue, jobTime(at), s.maxAttempts)
	if err != nil {
		return uuid.Nil, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO "+s.schema+".data_model_index_delivery_reservations(tenant_id,job_id,delivery_id) VALUES (?,?,?)", mysqlstore.UUID(tenant), mysqlstore.UUID(job), mysqlstore.UUID(id))
	if err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// Claim locks only one queue row and performs no domain work. Delivery work starts
// after commit. Lost commit acknowledgement returns no claim; expiry recovers it.
func (s *IndexDeliveryStore) Claim(ctx context.Context) (*DeliveryLease, error) {
	var result *DeliveryLease
	err := mysqlstore.Mutation(ctx, s.db, func(tx *sql.Tx) error {
		var v DeliveryLease
		err := tx.QueryRowContext(ctx, "SELECT id,tenant_id,job_id,attempt FROM "+s.schema+".data_model_index_deliveries WHERE queue_name=? AND state='available' AND available_at<=UTC_TIMESTAMP(6) AND attempt<max_attempts ORDER BY available_at,id LIMIT 1 FOR UPDATE SKIP LOCKED", s.queue).Scan((*mysqlstore.UUID)(&v.ID), (*mysqlstore.UUID)(&v.TenantID), (*mysqlstore.UUID)(&v.JobID), &v.Attempt)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		v.Token = uuid.New()
		if s.retryPolicy != nil {
			var count sql.NullInt64
			if err := tx.QueryRowContext(ctx, "SELECT error_count FROM "+s.schema+".data_model_index_deliveries WHERE tenant_id=? AND job_id=? AND id=?", mysqlstore.UUID(v.TenantID), mysqlstore.UUID(v.JobID), mysqlstore.UUID(v.ID)).Scan(&count); err != nil {
				return err
			}
			if !count.Valid || count.Int64 < 0 || count.Int64 > int64(v.Attempt) {
				return errors.New("delivery error history unknown or invalid; explicit translation required before claiming")
			}
		}
		v.Attempt++
		_, err = tx.ExecContext(ctx, "UPDATE "+s.schema+".data_model_index_deliveries SET state='running',attempt=?,lease_token=?,lease_until=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6)) WHERE tenant_id=? AND job_id=? AND id=?", v.Attempt, mysqlstore.UUID(v.Token), s.lease.Microseconds(), mysqlstore.UUID(v.TenantID), mysqlstore.UUID(v.JobID), mysqlstore.UUID(v.ID))
		if err == nil {
			result = &v
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func validLease(v DeliveryLease) bool {
	return v.ID != uuid.Nil && v.TenantID != uuid.Nil && v.JobID != uuid.Nil && v.Token != uuid.Nil && v.Attempt > 0
}
func (s *IndexDeliveryStore) Heartbeat(ctx context.Context, v DeliveryLease) error {
	if !validLease(v) {
		return ErrStaleDeliveryLease
	}
	result, err := s.db.ExecContext(ctx, "UPDATE "+s.schema+".data_model_index_deliveries SET lease_until=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6)),heartbeat_version=heartbeat_version+1 WHERE tenant_id=? AND job_id=? AND id=? AND queue_name=? AND state='running' AND lease_token=? AND attempt=? AND lease_until>UTC_TIMESTAMP(6) AND heartbeat_version<9223372036854775807", s.lease.Microseconds(), mysqlstore.UUID(v.TenantID), mysqlstore.UUID(v.JobID), mysqlstore.UUID(v.ID), s.queue, mysqlstore.UUID(v.Token), v.Attempt)
	return deliveryChanged(result, err)
}

func deliveryChanged(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrStaleDeliveryLease
	}
	return nil
}

func (s *IndexDeliveryStore) lockJob(ctx context.Context, tx *sql.Tx, tenant, job uuid.UUID, skip bool) error {
	query := "SELECT id FROM " + s.schema + ".data_model_index_jobs WHERE tenant_id=? AND id=? FOR UPDATE"
	if skip {
		query += " SKIP LOCKED"
	}
	var id mysqlstore.UUID
	return tx.QueryRowContext(ctx, query, mysqlstore.UUID(tenant), mysqlstore.UUID(job)).Scan(&id)
}

// Finish runs local effects and completion/retry in one transaction. Lock order:
// job -> delivery -> reservation. Claim/heartbeat lock delivery only. fn must be
// short local SQL on tx; no DDL, network calls, commit or lock-order inversion.
// The lease is rechecked after fn, so expiry rolls back ALL local effects.
func (s *IndexDeliveryStore) Finish(ctx context.Context, v DeliveryLease, retry bool, delay time.Duration, errorCode string, fn func(*sql.Tx) error) error {
	if s.retryPolicy != nil && delay != 0 {
		return errors.New("policy-controlled delivery cannot specify a fixed retry delay")
	}
	if !validLease(v) || delay < 0 || delay > 24*time.Hour || delay%time.Microsecond != 0 || !deliveryErrorPattern.MatchString(errorCode) {
		return errors.New("valid lease, retry delay and redacted error code required")
	}
	return mysqlstore.Mutation(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.lockJob(ctx, tx, v.TenantID, v.JobID, false); err != nil {
			return err
		}
		var maxAttempts int
		err := tx.QueryRowContext(ctx, "SELECT max_attempts FROM "+s.schema+".data_model_index_deliveries WHERE tenant_id=? AND job_id=? AND id=? AND queue_name=? AND state='running' AND lease_token=? AND attempt=? AND lease_until>UTC_TIMESTAMP(6) FOR UPDATE", mysqlstore.UUID(v.TenantID), mysqlstore.UUID(v.JobID), mysqlstore.UUID(v.ID), s.queue, mysqlstore.UUID(v.Token), v.Attempt).Scan(&maxAttempts)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrStaleDeliveryLease
		}
		if err != nil {
			return err
		}
		var priorErrors sql.NullInt64
		if s.retryPolicy != nil {
			if err := tx.QueryRowContext(ctx, "SELECT error_count FROM "+s.schema+".data_model_index_deliveries WHERE tenant_id=? AND job_id=? AND id=?", mysqlstore.UUID(v.TenantID), mysqlstore.UUID(v.JobID), mysqlstore.UUID(v.ID)).Scan(&priorErrors); err != nil {
				return err
			}
			if !priorErrors.Valid || priorErrors.Int64 < 0 || priorErrors.Int64 > int64(v.Attempt) {
				return errors.New("delivery error history unknown or invalid; explicit translation required")
			}
			if retry {
				if priorErrors.Int64 >= int64(v.Attempt) {
					return errors.New("delivery error count exceeds completed attempts")
				}
				if v.Attempt < maxAttempts {
					delay, err = s.retryPolicy.Delay(int(priorErrors.Int64))
					if err != nil {
						return err
					}
					if delay < 0 || delay%time.Microsecond != 0 {
						return errors.New("retry policy returned invalid delay")
					}
				}
			}
		}
		if fn != nil {
			if err := fn(tx); err != nil {
				return err
			}
		}
		state := "completed"
		retention := 24 * time.Hour
		if retry {
			state = "available"
			if v.Attempt >= maxAttempts {
				state = "discarded"
				retention = 7 * 24 * time.Hour
				errorCode = "delivery_attempts_exhausted"
			}
		}
		finalized := ",finalized_at=UTC_TIMESTAMP(6),retain_until=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6))"
		args := []any{state, delay.Microseconds(), errorCode, retention.Microseconds()}
		if state == "available" {
			finalized = ""
			args = args[:3]
		}
		args = append(args, mysqlstore.UUID(v.TenantID), mysqlstore.UUID(v.JobID), mysqlstore.UUID(v.ID), mysqlstore.UUID(v.Token), v.Attempt)
		counter := ""
		if s.retryPolicy != nil && retry {
			counter = ",error_count=error_count+1"
		}
		result, err := tx.ExecContext(ctx, "UPDATE "+s.schema+".data_model_index_deliveries SET state=?,available_at=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6)),last_error_code=?,lease_token=NULL,lease_until=NULL"+finalized+counter+" WHERE tenant_id=? AND job_id=? AND id=? AND state='running' AND lease_token=? AND attempt=? AND lease_until>UTC_TIMESTAMP(6)", args...)
		if err := deliveryChanged(result, err); err != nil {
			return err
		}
		if state == "discarded" {
			return s.release(ctx, tx, v)
		}
		return nil
	})
}

func (s *IndexDeliveryStore) release(ctx context.Context, tx *sql.Tx, v DeliveryLease) error {
	result, err := tx.ExecContext(ctx, "DELETE FROM "+s.schema+".data_model_index_delivery_reservations WHERE tenant_id=? AND job_id=? AND delivery_id=?", mysqlstore.UUID(v.TenantID), mysqlstore.UUID(v.JobID), mysqlstore.UUID(v.ID))
	return deliveryChanged(result, err)
}

// Cancel requires fresh operator authorization outside this adapter. Repeated
// cancellation returns without rerunning effects. Completed/discarded work must
// be inspected/redriven explicitly, not rewritten as cancelled.
func (s *IndexDeliveryStore) Cancel(ctx context.Context, tenant, job, id uuid.UUID, fn func(*sql.Tx) error) error {
	if tenant == uuid.Nil || job == uuid.Nil || id == uuid.Nil {
		return errors.New("tenant/job/delivery identities required")
	}
	return mysqlstore.Mutation(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.lockJob(ctx, tx, tenant, job, false); err != nil {
			return err
		}
		var state string
		if err := tx.QueryRowContext(ctx, "SELECT state FROM "+s.schema+".data_model_index_deliveries WHERE tenant_id=? AND job_id=? AND id=? AND queue_name=? FOR UPDATE", mysqlstore.UUID(tenant), mysqlstore.UUID(job), mysqlstore.UUID(id), s.queue).Scan(&state); err != nil {
			return err
		}
		if state == "cancelled" {
			return nil
		}
		if state == "completed" || state == "discarded" {
			return ErrDeliveryTerminal
		}
		if fn != nil {
			if err := fn(tx); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, "UPDATE "+s.schema+".data_model_index_deliveries SET state='cancelled',lease_token=NULL,lease_until=NULL,finalized_at=UTC_TIMESTAMP(6),retain_until=TIMESTAMPADD(DAY,1,UTC_TIMESTAMP(6)) WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(id))
		if err != nil {
			return err
		}
		return s.release(ctx, tx, DeliveryLease{ID: id, TenantID: tenant, JobID: job})
	})
}

// ReapExpired uses a separate expiry index, then a short transaction per candidate.
// Busy source jobs are skipped; claim never holds a queue lock while taking one.
func (s *IndexDeliveryStore) ReapExpired(ctx context.Context, limit int) (int, error) {
	n, _, err := s.MaintainPage(ctx, limit, false, nil)
	return n, err
}

// CleanupTerminal preserves River's 24h completed/cancelled and 7d discarded
// retention defaults. Completed reservations remain until this bounded cleanup.
func (s *IndexDeliveryStore) CleanupTerminal(ctx context.Context, limit int) (int, error) {
	n, _, err := s.MaintainPage(ctx, limit, true, nil)
	return n, err
}

// DeliveryMaintenanceCursor is a scan position, not authorization. A poller must
// continue through returned cursors before wrapping to nil, including pages where
// all source jobs were locked. This prevents a hot prefix starving later work.
type DeliveryMaintenanceCursor = ports.DeliveryMaintenanceCursor

var _ ports.DeliveryMaintenance = (*IndexDeliveryStore)(nil)

func (s *IndexDeliveryStore) MaintainPage(ctx context.Context, limit int, cleanup bool, after *DeliveryMaintenanceCursor) (int, *DeliveryMaintenanceCursor, error) {
	if limit < 1 || limit > 1000 {
		return 0, nil, errors.New("maintenance limit must be 1..1000")
	}
	predicate := "state='running' AND lease_until<=UTC_TIMESTAMP(6)"
	column := "lease_until"
	if cleanup {
		predicate = "retain_until<=UTC_TIMESTAMP(6)"
		column = "retain_until"
	}
	cursorPredicate := ""
	args := []any{s.queue}
	if after != nil {
		if after.Queue != s.queue || after.Cleanup != cleanup || after.ID == uuid.Nil {
			return 0, nil, errors.New("invalid delivery maintenance cursor scope")
		}
		at := mysqlstore.DateTime{Time: after.At}
		if _, err := at.Value(); err != nil {
			return 0, nil, err
		}
		cursorPredicate = " AND (" + column + ">? OR (" + column + "=? AND id>?))"
		args = append(args, at, at, mysqlstore.UUID(after.ID))
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, "SELECT id,tenant_id,job_id,"+column+" FROM "+s.schema+".data_model_index_deliveries WHERE queue_name=? AND "+predicate+cursorPredicate+" ORDER BY "+column+",id LIMIT ?", args...)
	if err != nil {
		return 0, nil, err
	}
	candidates := make([]DeliveryLease, 0, limit)
	var next *DeliveryMaintenanceCursor
	for rows.Next() {
		var v DeliveryLease
		var at mysqlstore.DateTime
		if err := rows.Scan((*mysqlstore.UUID)(&v.ID), (*mysqlstore.UUID)(&v.TenantID), (*mysqlstore.UUID)(&v.JobID), &at); err != nil {
			rows.Close()
			return 0, nil, err
		}
		candidates = append(candidates, v)
		next = &DeliveryMaintenanceCursor{Queue: s.queue, Cleanup: cleanup, At: at.Time, ID: v.ID}
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil {
		return 0, nil, err
	}
	if closeErr != nil {
		return 0, nil, closeErr
	}
	if len(candidates) < limit {
		next = nil
	}
	count := 0
	for _, v := range candidates {
		changed := false
		err := mysqlstore.Mutation(ctx, s.db, func(tx *sql.Tx) error {
			if err := s.lockJob(ctx, tx, v.TenantID, v.JobID, true); errors.Is(err, sql.ErrNoRows) {
				return nil
			} else if err != nil {
				return err
			}
			var state string
			var attempt, maxAttempts int
			err := tx.QueryRowContext(ctx, "SELECT state,attempt,max_attempts FROM "+s.schema+".data_model_index_deliveries WHERE tenant_id=? AND job_id=? AND id=? AND queue_name=? AND "+predicate+" FOR UPDATE SKIP LOCKED", mysqlstore.UUID(v.TenantID), mysqlstore.UUID(v.JobID), mysqlstore.UUID(v.ID), s.queue).Scan(&state, &attempt, &maxAttempts)
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			if cleanup {
				if state == "completed" {
					if err := s.release(ctx, tx, v); err != nil {
						return err
					}
				}
				result, err := tx.ExecContext(ctx, "DELETE FROM "+s.schema+".data_model_index_deliveries WHERE tenant_id=? AND id=?", mysqlstore.UUID(v.TenantID), mysqlstore.UUID(v.ID))
				if err := deliveryChanged(result, err); err != nil {
					return err
				}
			} else {
				delay := time.Duration(0)
				if s.retryPolicy != nil {
					var prior sql.NullInt64
					if err := tx.QueryRowContext(ctx, "SELECT error_count FROM "+s.schema+".data_model_index_deliveries WHERE tenant_id=? AND job_id=? AND id=?", mysqlstore.UUID(v.TenantID), mysqlstore.UUID(v.JobID), mysqlstore.UUID(v.ID)).Scan(&prior); err != nil {
						return err
					}
					if !prior.Valid || prior.Int64 < 0 || prior.Int64 >= int64(attempt) {
						return errors.New("delivery error history unknown or invalid; explicit translation required before rescue")
					}
					if attempt < maxAttempts {
						delay, err = s.retryPolicy.Delay(int(prior.Int64))
						if err != nil {
							return err
						}
						if delay < 0 || delay%time.Microsecond != 0 {
							return errors.New("retry policy returned invalid rescue delay")
						}
					}
					if _, err := tx.ExecContext(ctx, "UPDATE "+s.schema+".data_model_index_deliveries SET error_count=error_count+1 WHERE tenant_id=? AND job_id=? AND id=?", mysqlstore.UUID(v.TenantID), mysqlstore.UUID(v.JobID), mysqlstore.UUID(v.ID)); err != nil {
						return err
					}
				}
				if attempt >= maxAttempts {
					_, err = tx.ExecContext(ctx, "UPDATE "+s.schema+".data_model_index_deliveries SET state='discarded',lease_token=NULL,lease_until=NULL,last_error_code='lease_expired',finalized_at=UTC_TIMESTAMP(6),retain_until=TIMESTAMPADD(DAY,7,UTC_TIMESTAMP(6)) WHERE tenant_id=? AND job_id=? AND id=?", mysqlstore.UUID(v.TenantID), mysqlstore.UUID(v.JobID), mysqlstore.UUID(v.ID))
					if err != nil {
						return err
					}
					if err := s.release(ctx, tx, v); err != nil {
						return err
					}
				} else {
					_, err = tx.ExecContext(ctx, "UPDATE "+s.schema+".data_model_index_deliveries SET state='available',lease_token=NULL,lease_until=NULL,available_at=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6)),last_error_code='lease_expired' WHERE tenant_id=? AND job_id=? AND id=?", delay.Microseconds(), mysqlstore.UUID(v.TenantID), mysqlstore.UUID(v.JobID), mysqlstore.UUID(v.ID))
					if err != nil {
						return err
					}
				}
			}
			changed = true
			return nil
		})
		if err != nil {
			return count, nil, err
		}
		if changed {
			count++
		}
	}
	return count, next, nil
}
