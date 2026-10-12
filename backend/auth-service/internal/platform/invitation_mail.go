package platform

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type InvitationMailer interface {
	Send(context.Context, string, string, string) error
}
type SMTPMailer struct {
	Address, Username, Password, From string
	LocalCapture                      bool
}

func (m SMTPMailer) Validate() error {
	if _, _, err := net.SplitHostPort(m.Address); err != nil {
		return errors.New("SMTP address must include host and port")
	}
	if _, err := invitationEmail(m.From); err != nil {
		return errors.New("invalid SMTP sender")
	}
	if m.LocalCapture && (m.Username != "" || m.Password != "") {
		return errors.New("local mail capture must not have credentials")
	}
	return nil
}
func (m SMTPMailer) Send(ctx context.Context, to, link, messageID string) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if _, err := invitationEmail(to); err != nil {
		return err
	}
	host, _, _ := net.SplitHostPort(m.Address)
	conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", m.Address)
	if err != nil {
		return err
	}
	defer conn.Close()
	deadline := time.Now().Add(20 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err = conn.SetDeadline(deadline); err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Close()
	if !m.LocalCapture {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("SMTP requires STARTTLS")
		}
		if err = c.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}
	if m.Username != "" {
		if err = c.Auth(smtp.PlainAuth("", m.Username, m.Password, host)); err != nil {
			return err
		}
	}
	if err = c.Mail(m.From); err != nil {
		return err
	}
	if err = c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	// Only validated addresses and server-generated IDs/origin enter the message.
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: Invitation to ITC Fraud System\r\nMessage-ID: <%s@fraud-invitations>\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nYou have been invited to ITC Fraud System.\r\nOpen this link and sign in using this email address:\r\n\r\n%s\r\n\r\nThis invitation expires in 48 hours and can be used once.\r\nIf you did not expect it, you can ignore this message.\r\n", m.From, to, messageID, link)
	if _, err = w.Write([]byte(msg)); err != nil {
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}
	// DATA acknowledgement establishes relay acceptance, not inbox delivery.
	_ = c.Quit()
	return nil
}

// DeliverOne claims one row with SKIP LOCKED and releases its transaction before
// network IO. The lease/generation fence prevents stale completion after resend.
// SMTP is at-least-once: a crash after DATA may duplicate mail, never membership.
func (i *InvitationService) DeliverOne(ctx context.Context, m InvitationMailer) (bool, error) {
	tx, err := i.Store.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(context.Background())
	var id uuid.UUID
	var generation int64
	var payload []byte
	var attempts int
	err = tx.QueryRow(ctx, `SELECT invitation_id,generation,payload,attempts FROM identity.invitation_delivery WHERE (state='queued' AND next_attempt<=now()) OR (state='sending' AND lease_until<=now()) ORDER BY next_attempt LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&id, &generation, &payload, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var valid bool
	if err = tx.QueryRow(ctx, `SELECT status='pending' AND expires_at>now() AND version=$2 FROM identity.invitations WHERE id=$1`, id, generation).Scan(&valid); err != nil {
		return false, err
	}
	if !valid || attempts >= 5 {
		state := "cancelled"
		if valid {
			state = "failed"
		}
		if _, err = tx.Exec(ctx, `UPDATE identity.invitation_delivery SET state=$2,payload=NULL,lease_id=NULL,lease_until=NULL WHERE invitation_id=$1`, id, state); err != nil {
			return false, err
		}
		return true, tx.Commit(ctx)
	}
	lease := uuid.New()
	if _, err = tx.Exec(ctx, `UPDATE identity.invitation_delivery SET state='sending',attempts=attempts+1,lease_id=$2,lease_until=now()+interval '1 minute' WHERE invitation_id=$1`, id, lease); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	p, sendErr := i.open(id, payload)
	if sendErr == nil {
		sendErr = m.Send(ctx, p.Email, i.Origin+"/invite#"+p.Token, fmt.Sprintf("%s.%d", id, generation))
	}
	state := "sent"
	if sendErr != nil {
		state = "queued"
		if attempts+1 >= 5 {
			state = "failed"
		}
	}
	// No provider errors, recipients or secrets go to generic logs/receipts.
	_, err = i.Store.DB.Exec(ctx, `UPDATE identity.invitation_delivery SET state=$4,payload=CASE WHEN $4 IN ('sent','failed') THEN NULL ELSE payload END,lease_id=NULL,lease_until=NULL,next_attempt=now()+($5 * interval '1 minute'),sent_at=CASE WHEN $4='sent' THEN now() ELSE NULL END WHERE invitation_id=$1 AND generation=$2 AND lease_id=$3`, id, generation, lease, state, 1<<attempts)
	return true, err
}
func (i *InvitationService) RunDelivery(ctx context.Context, m InvitationMailer) {
	timer := time.NewTicker(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			jobCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
			_, err := i.DeliverOne(jobCtx, m)
			if err != nil && ctx.Err() == nil {
				slog.Error("invitation delivery storage operation failed")
			}
			cancel()
		}
	}
}

func SMTPFromEnvironment(get func(string) string) (SMTPMailer, error) {
	m := SMTPMailer{Address: get("AUTH_SMTP_ADDR"), Username: get("AUTH_SMTP_USERNAME"), Password: get("AUTH_SMTP_PASSWORD"), From: get("AUTH_SMTP_FROM"), LocalCapture: get("AUTH_SMTP_LOCAL_CAPTURE") == "true"}
	if strings.TrimSpace(m.Address) == "" {
		return m, errors.New("invitation SMTP configuration required")
	}
	return m, m.Validate()
}
