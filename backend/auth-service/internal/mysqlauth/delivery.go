package mysqlauth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/internal/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
	"log/slog"
	"time"
)

// DeliverOne performs network I/O only after committing a bounded SKIP LOCKED
// claim. A previous lease/generation can never acknowledge newer work.
func (i *Invitations) DeliverOne(ctx context.Context, mail platform.InvitationMailer) (bool, error) {
	if mail == nil {
		return false, errors.New("invitation mailer required")
	}
	s := i.Store
	var invite mysqlstore.UUID
	var generation int64
	var attempts int
	var payload []byte
	lease := uuid.New()
	claimed := false
	send := false
	e := s.transaction(ctx, func(tx *sql.Tx) error {
		// Separate index ranges avoid an unbounded OR scan of retained sent history.
		for _, candidate := range []struct{ predicate, order string }{{"state='sending' AND lease_until<=SYSDATE(6)", "lease_until"}, {"state='queued' AND next_attempt<=SYSDATE(6)", "next_attempt"}} {
			e := tx.QueryRowContext(ctx, "SELECT invitation_id,generation,payload,attempts FROM "+s.table("identity_invitation_delivery")+" WHERE "+candidate.predicate+" ORDER BY "+candidate.order+",invitation_id LIMIT 1 FOR UPDATE SKIP LOCKED").Scan(&invite, &generation, &payload, &attempts)
			if errors.Is(e, sql.ErrNoRows) {
				continue
			}
			if e != nil {
				return e
			}
			claimed = true
			break
		}
		if !claimed {
			return nil
		}
		var valid bool
		if e := tx.QueryRowContext(ctx, "SELECT status='pending' AND expires_at>SYSDATE(6) AND version=? FROM "+s.table("identity_invitations")+" WHERE id=?", generation, invite).Scan(&valid); e != nil {
			return e
		}
		if !valid || attempts >= 5 {
			state := "cancelled"
			if valid {
				state = "failed"
			}
			_, e := tx.ExecContext(ctx, "UPDATE "+s.table("identity_invitation_delivery")+" SET state=?,payload=NULL,lease_id=NULL,lease_until=NULL WHERE invitation_id=?", state, invite)
			return e
		}
		_, e := tx.ExecContext(ctx, "UPDATE "+s.table("identity_invitation_delivery")+" SET state='sending',attempts=attempts+1,lease_id=?,lease_until=TIMESTAMPADD(MINUTE,1,SYSDATE(6)) WHERE invitation_id=?", id(lease), invite)
		send = e == nil
		return e
	})
	if e != nil || !send {
		return claimed, e
	}
	p, sendErr := i.Envelope.Open(uuid.UUID(invite), payload)
	if sendErr == nil {
		sendErr = mail.Send(ctx, p.Email, i.Envelope.Origin+"/invite#"+p.Token, fmt.Sprintf("%s.%d", uuid.UUID(invite), generation))
	}
	state := "sent"
	if sendErr != nil {
		state = "queued"
		if attempts+1 >= 5 {
			state = "failed"
		}
	}
	// The post-network result is bounded by both lease identity and wall clock.
	r, e := s.DB.ExecContext(ctx, "UPDATE "+s.table("identity_invitation_delivery")+" SET state=?,payload=CASE WHEN ? IN ('sent','failed') THEN NULL ELSE payload END,lease_id=NULL,lease_until=NULL,next_attempt=TIMESTAMPADD(MINUTE,?,SYSDATE(6)),sent_at=CASE WHEN ?='sent' THEN SYSDATE(6) ELSE NULL END WHERE invitation_id=? AND generation=? AND lease_id=? AND lease_until>SYSDATE(6) AND state='sending'", state, state, 1<<attempts, state, invite, generation, id(lease))
	if e == nil {
		n, err := r.RowsAffected()
		e = err
		if e == nil && n != 1 {
			e = ErrDeliveryFenced
		}
	}
	return true, e
}

var ErrDeliveryFenced = errors.New("invitation delivery lease or generation superseded")

func (i *Invitations) Run(ctx context.Context, mail platform.InvitationMailer, logger *slog.Logger) error {
	if mail == nil {
		return errors.New("invitation mailer required")
	}
	timer := time.NewTicker(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			jobCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
			_, e := i.DeliverOne(jobCtx, mail)
			if e == nil {
				_, e = i.Store.PurgeExpiredLogins(jobCtx)
			}
			cancel()
			if e != nil && ctx.Err() == nil {
				logger.Error("auth invitation worker storage operation failed")
			}
		}
	}
}

func (s *Store) PurgeExpiredLogins(ctx context.Context) (int64, error) {
	r, e := s.DB.ExecContext(ctx, "DELETE FROM "+s.table("identity_login_transactions")+" WHERE expires_at<=SYSDATE(6) ORDER BY expires_at,digest LIMIT 100")
	if e != nil {
		return 0, e
	}
	return r.RowsAffected()
}
