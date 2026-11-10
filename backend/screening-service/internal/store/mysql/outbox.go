package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/google/uuid"
	"time"
)

func (r outbox) Enqueue(ctx context.Context, event, tenant, kind string, payload any) error {
	b, e := encoded(payload)
	if e != nil {
		return e
	}
	_, e = r.q.ExecContext(ctx, "INSERT INTO "+r.table("case_event_outbox")+"(tenant_id,id,kind,payload,next_attempt_at,created_at) VALUES(?,?,?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6)) ON DUPLICATE KEY UPDATE id=id", id(tenant), id(event), kind, b)
	if e != nil {
		return normalize(e)
	}
	var oldKind string
	var old []byte
	if e = r.q.QueryRowContext(ctx, "SELECT kind,payload FROM "+r.table("case_event_outbox")+" WHERE tenant_id=? AND id=?", id(tenant), id(event)).Scan(&oldKind, &old); e != nil {
		return normalize(e)
	}
	var a, c any
	if json.Unmarshal(old, &a) != nil || json.Unmarshal(b, &c) != nil {
		return errors.New("invalid outbox payload")
	}
	canonicalA, _ := json.Marshal(a)
	canonicalB, _ := json.Marshal(c)
	if oldKind != kind || string(canonicalA) != string(canonicalB) {
		return errors.New("callback event identity conflict")
	}
	return nil
}
func (s *Store) Claim(ctx context.Context) (out *ports.CaseEvent, err error) {
	err = s.Run(ctx, func(m ports.MutationStore) error {
		r := m.(*Store)
		v := ports.CaseEvent{LeaseToken: uuid.NewString()}
		e := r.q.QueryRowContext(ctx, "SELECT tenant_id,id,kind,payload,attempts FROM "+r.table("case_event_outbox")+" WHERE status='pending' AND next_attempt_at<=UTC_TIMESTAMP(6) AND(lease_until IS NULL OR lease_until<=UTC_TIMESTAMP(6)) ORDER BY next_attempt_at,created_at,id LIMIT 1 FOR UPDATE SKIP LOCKED").Scan(uid(&v.TenantID), uid(&v.ID), &v.Kind, &v.Payload, &v.Attempts)
		if errors.Is(e, sql.ErrNoRows) {
			return nil
		}
		if e != nil {
			return e
		}
		v.Attempts++
		_, e = r.q.ExecContext(ctx, "UPDATE "+r.table("case_event_outbox")+" SET attempts=?,lease_token=?,lease_until=TIMESTAMPADD(SECOND,60,UTC_TIMESTAMP(6)) WHERE tenant_id=? AND id=?", v.Attempts, id(v.LeaseToken), id(v.TenantID), id(v.ID))
		if e == nil {
			out = &v
		}
		return e
	})
	return
}
func (s *Store) Finish(ctx context.Context, v ports.CaseEvent, deliveryErr error) error {
	status, message := "sent", ""
	delay := time.Second * time.Duration(1<<min(v.Attempts, 8))
	if deliveryErr != nil {
		status = "pending"
		message = "case callback delivery failed"
		if v.Attempts >= 12 {
			status = "failed"
		}
	}
	result, e := s.q.ExecContext(ctx, "UPDATE "+s.table("case_event_outbox")+" SET status=?,last_error=?,next_attempt_at=TIMESTAMPADD(SECOND,?,UTC_TIMESTAMP(6)),lease_token=NULL,lease_until=NULL,sent_at=IF(?='sent',UTC_TIMESTAMP(6),NULL) WHERE tenant_id=? AND id=? AND status='pending' AND lease_token=? AND lease_until>UTC_TIMESTAMP(6)", status, message, int(delay/time.Second), status, id(v.TenantID), id(v.ID), id(v.LeaseToken))
	if e != nil {
		return normalize(e)
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return ports.ErrScreeningBusy
	}
	return nil
}
func (s *Store) ReplayCallback(ctx context.Context, tenant, event string) error {
	result, e := s.q.ExecContext(ctx, "UPDATE "+s.table("case_event_outbox")+" SET status='pending',attempts=0,last_error='',next_attempt_at=UTC_TIMESTAMP(6),lease_token=NULL,lease_until=NULL,sent_at=NULL WHERE tenant_id=? AND id=? AND status IN('sent','failed') AND(lease_until IS NULL OR lease_until<=UTC_TIMESTAMP(6))", id(tenant), id(event))
	if e != nil {
		return normalize(e)
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return errors.New("terminal unleased callback required")
	}
	return nil
}

// ClaimWork coordinates independent worker replicas without a network call or
// a tenant-wide mutex. Completion must present this token before any writes.
func (s *Store) ClaimWork(ctx context.Context, t, kind, key string) (token string, err error) {
	token = uuid.NewString()
	err = s.Run(ctx, func(m ports.MutationStore) error {
		r := m.(*Store)
		_, e := r.q.ExecContext(ctx, "INSERT INTO "+r.table("work_leases")+"(tenant_id,kind,id,token,lease_until) VALUES(?,?,?,?,UTC_TIMESTAMP(6)) ON DUPLICATE KEY UPDATE id=id", id(t), kind, id(key), id(token))
		if e != nil {
			return e
		}
		var until time.Time
		if e = r.q.QueryRowContext(ctx, "SELECT lease_until FROM "+r.table("work_leases")+" WHERE tenant_id=? AND kind=? AND id=? FOR UPDATE", id(t), kind, id(key)).Scan(&until); e != nil {
			return e
		}
		if until.After(time.Now().UTC()) {
			return ports.ErrScreeningBusy
		}
		_, e = r.q.ExecContext(ctx, "UPDATE "+r.table("work_leases")+" SET token=?,attempts=attempts+1,last_error='',lease_until=TIMESTAMPADD(SECOND,60,UTC_TIMESTAMP(6)) WHERE tenant_id=? AND kind=? AND id=?", id(token), id(t), kind, id(key))
		return e
	})
	return
}
func (s *Store) LockWork(ctx context.Context, t, kind, key, token string) error {
	var found mysqlstore.UUID
	e := s.q.QueryRowContext(ctx, "SELECT id FROM "+s.table("work_leases")+" WHERE tenant_id=? AND kind=? AND id=? AND token=? AND lease_until>UTC_TIMESTAMP(6) FOR UPDATE", id(t), kind, id(key), id(token)).Scan(&found)
	if e != nil {
		return ports.ErrScreeningBusy
	}
	return nil
}
func (s *Store) ReleaseWork(ctx context.Context, t, kind, key, token string) error {
	_, e := s.q.ExecContext(ctx, "DELETE FROM "+s.table("work_leases")+" WHERE tenant_id=? AND kind=? AND id=? AND token=?", id(t), kind, id(key), id(token))
	return normalize(e)
}

func (s *Store) BackoffWork(ctx context.Context, t, kind, key, token string) error {
	return s.Run(ctx, func(m ports.MutationStore) error {
		r := m.(*Store)
		if e := r.LockWork(ctx, t, kind, key, token); e != nil {
			return e
		}
		var attempts int
		if e := r.q.QueryRowContext(ctx, "SELECT attempts FROM "+r.table("work_leases")+" WHERE tenant_id=? AND kind=? AND id=?", id(t), kind, id(key)).Scan(&attempts); e != nil {
			return e
		}
		delay := min(300, 1<<min(attempts+1, 8))
		if kind == "continuous" && attempts >= 10 {
			if _, e := r.q.ExecContext(ctx, "UPDATE "+r.table("monitored_objects")+" SET status='paused',updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=? AND status='pending'", id(t), id(key)); e != nil {
				return e
			}
			delay = 0
		}
		_, e := r.q.ExecContext(ctx, "UPDATE "+r.table("work_leases")+" SET lease_until=TIMESTAMPADD(SECOND,?,UTC_TIMESTAMP(6)),last_error='worker processing failed' WHERE tenant_id=? AND kind=? AND id=? AND token=?", delay, id(t), kind, id(key), id(token))
		return e
	})
}
