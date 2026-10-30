// Package work provides shared transaction-bound MySQL work mechanics.
// Services own their tables, authorization, handlers and business transitions.
package work

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"time"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var ErrLease = errors.New("work lease is absent, expired or stale")
var ErrActive = errors.New("resource already has active work")
var queueName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

func ValidQueueName(s string) bool { return queueName.MatchString(s) }

var name = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)

type Queue struct {
	rescueQueue        string
	tenant             uuid.UUID
	db                 *sql.DB
	jobs, reservations string
	maxPayload         int
}
type Job struct {
	ID, TenantID, ResourceID uuid.UUID
	Kind, Queue              string
	Version                  int
	Payload                  json.RawMessage
	AvailableAt              time.Time
	MaxAttempts              int
}
type Lease struct {
	// QueueWait measures database time since this attempt became available.
	// Extremely old retained timestamps saturate at the maximum Go duration.
	QueueWait time.Duration
	Job
	Token   uuid.UUID
	Attempt int
	Until   time.Time
}

func New(db *sql.DB, database, owner string, maxPayload int) (*Queue, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if db == nil || (!name.MatchString(owner) || len(owner) > 32) || maxPayload < 1 || maxPayload > 1<<20 {
		return nil, errors.New("scoped queue and payload limit required")
	}
	return &Queue{db: db, jobs: s + ".`" + owner + "_work_jobs`", reservations: s + ".`" + owner + "_work_reservations`", maxPayload: maxPayload}, nil
}

// NewScoped binds claim and rescue scans to one authorized tenant. It does not
// confer authority on business handlers; they must still authorize every lease.
func NewScoped(db *sql.DB, database, owner string, maxPayload int, tenant uuid.UUID) (*Queue, error) {
	if tenant == uuid.Nil {
		return nil, errors.New("tenant scope required")
	}
	q, err := New(db, database, owner, maxPayload)
	if err != nil {
		return nil, err
	}
	q.tenant = tenant
	return q, nil
}

// Schema is owner-owned staged DDL. Installation, tenant lineage and grants are
// the owner's responsibility; it must live in the shared application database.
func Schema(database, owner string) ([]string, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if !name.MatchString(owner) || len(owner) > 32 {
		return nil, errors.New("invalid work owner")
	}
	j, r := s+".`"+owner+"_work_jobs`", s+".`"+owner+"_work_reservations`"
	return []string{
		"CREATE TABLE " + j + ` (
id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,resource_id BINARY(16) NOT NULL,
kind VARCHAR(48) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
queue VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,payload_version INT NOT NULL,
payload JSON NOT NULL,state VARCHAR(12) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
available_at DATETIME(6) NOT NULL,attempt INT NOT NULL DEFAULT 0,max_attempts INT NOT NULL,
lease_token BINARY(16) NULL,lease_until DATETIME(6) NULL,
created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),started_at DATETIME(6) NULL,finished_at DATETIME(6) NULL,
last_error VARCHAR(256) NULL,
UNIQUE(tenant_id,id),KEY ready(queue,state,available_at,id),KEY expired(state,lease_until,id),
KEY terminal(state,finished_at,id),KEY resource(tenant_id,resource_id,created_at,id),
CHECK(payload_version>0),CHECK(attempt>=0 AND max_attempts BETWEEN 1 AND 100),
CHECK(state IN ('available','running','completed','discarded','cancelled')),
CHECK((state='running' AND lease_token IS NOT NULL AND lease_until IS NOT NULL) OR
(state<>'running' AND lease_token IS NULL AND lease_until IS NULL)),
CHECK((state IN ('completed','discarded','cancelled') AND finished_at IS NOT NULL) OR
(state IN ('available','running') AND finished_at IS NULL)),CHECK(JSON_TYPE(payload)='OBJECT')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin`,
		"CREATE TABLE " + r + ` (tenant_id BINARY(16) NOT NULL,kind VARCHAR(48) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
resource_id BINARY(16) NOT NULL,job_id BINARY(16) NOT NULL,
PRIMARY KEY(tenant_id,kind,resource_id),UNIQUE(job_id),
FOREIGN KEY(tenant_id,job_id) REFERENCES ` + j + `(tenant_id,id)) ENGINE=InnoDB`,
	}, nil
}

// EnqueueTx joins the caller's business transaction. A uniqueness failure must
// be propagated to roll back that transaction; no generic unique-key upsert.
func (q *Queue) EnqueueTx(ctx context.Context, tx *sql.Tx, j Job) error {
	if q == nil || tx == nil || j.ID == uuid.Nil || j.TenantID == uuid.Nil || j.ResourceID == uuid.Nil || !name.MatchString(j.Kind) || !ValidQueueName(j.Queue) || j.Version < 1 || j.MaxAttempts < 1 || j.MaxAttempts > 100 || len(j.Payload) > q.maxPayload || !utf8.Valid(j.Payload) || !json.Valid(j.Payload) {
		return errors.New("invalid bounded work command")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(j.Payload, &object); err != nil || object == nil {
		return errors.New("work payload must be object")
	}
	at, err := (mysqlstore.DateTime{Time: j.AvailableAt}).Value()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO "+q.jobs+"(id,tenant_id,resource_id,kind,queue,payload_version,payload,state,available_at,max_attempts) VALUES(?,?,?,?,?,?,?,'available',?,?)", mysqlstore.UUID(j.ID), mysqlstore.UUID(j.TenantID), mysqlstore.UUID(j.ResourceID), j.Kind, j.Queue, j.Version, string(j.Payload), at, j.MaxAttempts)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO "+q.reservations+"(tenant_id,kind,resource_id,job_id) VALUES(?,?,?,?)", mysqlstore.UUID(j.TenantID), j.Kind, mysqlstore.UUID(j.ResourceID), mysqlstore.UUID(j.ID))
	if mysqlstore.Classify(err) == mysqlstore.Duplicate {
		return errors.Join(ErrActive, err)
	}
	return err
}

func (q *Queue) Claim(ctx context.Context, queue string, limit int, ttl time.Duration) ([]Lease, error) {
	if q == nil || !ValidQueueName(queue) || limit < 1 || limit > 100 || ttl < time.Second || ttl > 30*time.Minute {
		return nil, errors.New("bounded queue claim required")
	}
	var leases []Lease
	err := mysqlstore.Mutation(ctx, q.db, func(tx *sql.Tx) error {
		scope := ""
		args := []any{q.maxPayload, queue}
		if q.tenant != uuid.Nil {
			scope = " AND tenant_id=?"
			args = append(args, mysqlstore.UUID(q.tenant))
		}
		args = append(args, limit)
		rows, err := tx.QueryContext(ctx, "SELECT id,tenant_id,resource_id,kind,payload_version,OCTET_LENGTH(payload),CASE WHEN OCTET_LENGTH(payload)<=? THEN payload ELSE NULL END,available_at,attempt,max_attempts FROM "+q.jobs+" WHERE queue=? AND state='available' AND available_at<=UTC_TIMESTAMP(6) AND attempt<max_attempts"+scope+" ORDER BY available_at,id LIMIT ? FOR UPDATE SKIP LOCKED", args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, tenant, resource mysqlstore.UUID
			var at mysqlstore.DateTime
			var l Lease
			var payloadSize int
			if err := rows.Scan(&id, &tenant, &resource, &l.Kind, &l.Version, &payloadSize, &l.Payload, &at, &l.Attempt, &l.MaxAttempts); err != nil {
				rows.Close()
				return err
			}
			if payloadSize > q.maxPayload {
				rows.Close()
				return errors.New("retained work payload exceeds limit")
			}
			l.ID = uuid.UUID(id)
			l.TenantID = uuid.UUID(tenant)
			l.ResourceID = uuid.UUID(resource)
			l.AvailableAt = at.Time
			l.Queue = queue
			l.Token = uuid.New()
			l.Attempt++
			leases = append(leases, l)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for i := range leases {
			l := &leases[i]
			if _, err := tx.ExecContext(ctx, "UPDATE "+q.jobs+" SET state='running',attempt=attempt+1,started_at=UTC_TIMESTAMP(6),lease_token=?,lease_until=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6)) WHERE id=?", mysqlstore.UUID(l.Token), ttl.Microseconds(), mysqlstore.UUID(l.ID)); err != nil {
				return err
			}
			var until mysqlstore.DateTime
			var waitMicros int64
			if err := tx.QueryRowContext(ctx, "SELECT lease_until,GREATEST(0,TIMESTAMPDIFF(MICROSECOND,available_at,UTC_TIMESTAMP(6))) FROM "+q.jobs+" WHERE id=?", mysqlstore.UUID(l.ID)).Scan(&until, &waitMicros); err != nil {
				return err
			}
			l.Until = until.Time
			const maxDuration = time.Duration(1<<63 - 1)
			if waitMicros > int64(maxDuration/time.Microsecond) {
				l.QueueWait = maxDuration
			} else {
				l.QueueWait = time.Duration(waitMicros) * time.Microsecond
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return leases, nil
}

// FenceTx must run BEFORE local business locks and remain held through commit.
// It reloads immutable job identity; caller-controlled Lease fields are checked.
func (q *Queue) FenceTx(ctx context.Context, tx *sql.Tx, l Lease) error {
	if q == nil || tx == nil || l.ID == uuid.Nil || l.Token == uuid.Nil {
		return ErrLease
	}
	var resource mysqlstore.UUID
	var kind, queue string
	var version, attempt, maxAttempts int
	err := tx.QueryRowContext(ctx, "SELECT resource_id,kind,queue,payload_version,attempt,max_attempts FROM "+q.jobs+" WHERE id=? AND tenant_id=? AND state='running' AND lease_token=? AND lease_until>UTC_TIMESTAMP(6) FOR UPDATE", mysqlstore.UUID(l.ID), mysqlstore.UUID(l.TenantID), mysqlstore.UUID(l.Token)).Scan(&resource, &kind, &queue, &version, &attempt, &maxAttempts)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrLease
	}
	if err != nil {
		return err
	}
	if uuid.UUID(resource) != l.ResourceID || kind != l.Kind || queue != l.Queue || version != l.Version || attempt != l.Attempt || maxAttempts != l.MaxAttempts {
		return ErrLease
	}
	return nil
}

func (q *Queue) CompleteTx(ctx context.Context, tx *sql.Tx, l Lease) error {
	return q.terminalTx(ctx, tx, l, "completed", "")
}
func (q *Queue) DiscardTx(ctx context.Context, tx *sql.Tx, l Lease, code string) error {
	return q.terminalTx(ctx, tx, l, "discarded", code)
}
func (q *Queue) terminalTx(ctx context.Context, tx *sql.Tx, l Lease, state, code string) error {
	if !utf8.ValidString(code) || len(code) > 256 {
		return errors.New("bounded redacted work error required")
	}
	if err := q.FenceTx(ctx, tx, l); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "UPDATE "+q.jobs+" SET state=?,finished_at=UTC_TIMESTAMP(6),lease_token=NULL,lease_until=NULL,last_error=? WHERE id=? AND lease_token=? AND lease_until>UTC_TIMESTAMP(6)", state, code, mysqlstore.UUID(l.ID), mysqlstore.UUID(l.Token))
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrLease
	}
	return q.release(ctx, tx, l.Job)
}
func (q *Queue) release(ctx context.Context, tx *sql.Tx, j Job) error {
	r, err := tx.ExecContext(ctx, "DELETE FROM "+q.reservations+" WHERE tenant_id=? AND kind=? AND resource_id=? AND job_id=?", mysqlstore.UUID(j.TenantID), j.Kind, mysqlstore.UUID(j.ResourceID), mysqlstore.UUID(j.ID))
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("active work reservation missing")
	}
	return nil
}

func (q *Queue) Heartbeat(ctx context.Context, l Lease, ttl time.Duration) error {
	if ttl < time.Second || ttl > 30*time.Minute {
		return errors.New("invalid work lease duration")
	}
	return mysqlstore.Mutation(ctx, q.db, func(tx *sql.Tx) error {
		if err := q.FenceTx(ctx, tx, l); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE "+q.jobs+" SET lease_until=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6)) WHERE id=?", ttl.Microseconds(), mysqlstore.UUID(l.ID))
		return err
	})
}

func (q *Queue) Retry(ctx context.Context, l Lease, delay time.Duration, code string) error {
	if delay < time.Second || delay > 24*time.Hour || !utf8.ValidString(code) || len(code) > 256 {
		return errors.New("bounded retry delay/error required")
	}
	return mysqlstore.Mutation(ctx, q.db, func(tx *sql.Tx) error {
		if err := q.FenceTx(ctx, tx, l); err != nil {
			return err
		}
		if l.Attempt >= l.MaxAttempts {
			return q.DiscardTx(ctx, tx, l, code)
		}
		_, err := tx.ExecContext(ctx, "UPDATE "+q.jobs+" SET state='available',available_at=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6)),lease_token=NULL,lease_until=NULL,last_error=? WHERE id=?", delay.Microseconds(), code, mysqlstore.UUID(l.ID))
		return err
	})
}

// Reap is a bounded, independently indexed expired-lease scan. Old tokens are
// invalidated under the same row lock used by business-effect fencing.
func (q *Queue) Reap(ctx context.Context, limit int, delay time.Duration) (int, error) {
	if limit < 1 || limit > 100 || delay < time.Second || delay > 24*time.Hour {
		return 0, errors.New("bounded reaper required")
	}
	count := 0
	err := mysqlstore.Mutation(ctx, q.db, func(tx *sql.Tx) error {
		scope := ""
		args := []any{}
		if q.tenant != uuid.Nil {
			scope = " AND tenant_id=?"
			args = append(args, mysqlstore.UUID(q.tenant))
		}
		if q.rescueQueue != "" {
			scope += " AND queue=?"
			args = append(args, q.rescueQueue)
		}
		args = append(args, limit)
		rows, err := tx.QueryContext(ctx, "SELECT id,tenant_id,resource_id,kind,attempt,max_attempts FROM "+q.jobs+" WHERE state='running' AND lease_until<=UTC_TIMESTAMP(6)"+scope+" ORDER BY lease_until,id LIMIT ? FOR UPDATE SKIP LOCKED", args...)
		if err != nil {
			return err
		}
		var expired []Lease
		for rows.Next() {
			var id, tenant, resource mysqlstore.UUID
			var l Lease
			if err := rows.Scan(&id, &tenant, &resource, &l.Kind, &l.Attempt, &l.MaxAttempts); err != nil {
				rows.Close()
				return err
			}
			l.ID = uuid.UUID(id)
			l.TenantID = uuid.UUID(tenant)
			l.ResourceID = uuid.UUID(resource)
			expired = append(expired, l)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, l := range expired {
			if l.Attempt >= l.MaxAttempts {
				if _, err := tx.ExecContext(ctx, "UPDATE "+q.jobs+" SET state='discarded',finished_at=UTC_TIMESTAMP(6),lease_token=NULL,lease_until=NULL,last_error='lease_expired' WHERE id=?", mysqlstore.UUID(l.ID)); err != nil {
					return err
				}
				if err := q.release(ctx, tx, l.Job); err != nil {
					return err
				}
			} else {
				if _, err := tx.ExecContext(ctx, "UPDATE "+q.jobs+" SET state='available',available_at=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6)),lease_token=NULL,lease_until=NULL,last_error='lease_expired' WHERE id=?", delay.Microseconds(), mysqlstore.UUID(l.ID)); err != nil {
					return err
				}
			}
			count++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}
