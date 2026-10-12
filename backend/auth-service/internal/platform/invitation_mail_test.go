package platform

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSMTPRequiresTLSExceptExplicitCapture(t *testing.T) {
	for _, capture := range []bool{false, true} {
		t.Run(fmt.Sprint(capture), func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			message := make(chan string, 1)
			go func() {
				c, e := listener.Accept()
				if e != nil {
					message <- ""
					return
				}
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(3 * time.Second))
				_, _ = fmt.Fprint(c, "220 local test SMTP\r\n")
				r := bufio.NewReader(c)
				data := ""
				for {
					line, e := r.ReadString('\n')
					if e != nil {
						message <- data
						return
					}
					switch {
					case strings.HasPrefix(line, "EHLO"):
						_, _ = fmt.Fprint(c, "250 local test\r\n")
					case strings.HasPrefix(line, "MAIL"), strings.HasPrefix(line, "RCPT"):
						_, _ = fmt.Fprint(c, "250 OK\r\n")
					case strings.HasPrefix(line, "DATA"):
						_, _ = fmt.Fprint(c, "354 continue\r\n")
						for {
							line, e = r.ReadString('\n')
							if e != nil {
								message <- data
								return
							}
							if line == ".\r\n" {
								break
							}
							data += line
						}
						_, _ = fmt.Fprint(c, "250 queued\r\n")
					case strings.HasPrefix(line, "QUIT"):
						_, _ = fmt.Fprint(c, "221 bye\r\n")
						message <- data
						return
					default:
						_, _ = fmt.Fprint(c, "500 unsupported\r\n")
					}
				}
			}()
			m := SMTPMailer{Address: listener.Addr().String(), From: "access@example.test", LocalCapture: capture}
			err = m.Send(context.Background(), "user@example.test", "https://app.test/invite#token", "test-message")
			if (err == nil) != capture {
				t.Fatalf("capture=%v err=%v", capture, err)
			}
			data := <-message
			if capture && !strings.Contains(data, "https://app.test/invite#token") {
				t.Fatal("SMTP message missing link")
			}
			if !capture && data != "" {
				t.Fatal("sent without TLS")
			}
		})
	}
}

type mailFunc func(context.Context, string, string, string) error

func (f mailFunc) Send(c context.Context, to, link, id string) error { return f(c, to, link, id) }
func TestDeliveryResendFencesOldCompletionAndBoundsFailures(t *testing.T) {
	i, _, raw := invitationFixture(t)
	ctx := context.Background()
	id := createInvite(t, i, raw, "fence@example.test")
	old := invitationToken(t, i, id)
	_, err := i.Store.DB.Exec(ctx, `UPDATE identity.invitation_delivery SET requested_at=now()-interval '2 minutes' WHERE invitation_id=$1`, id)
	if err != nil {
		t.Fatal(err)
	}
	m := mailFunc(func(context.Context, string, string, string) error { return i.Change(ctx, raw, id, 1, "resend") })
	if _, err = i.DeliverOne(ctx, m); err != nil {
		t.Fatal(err)
	}
	var generation int
	var state string
	var payload []byte
	if err = i.Store.DB.QueryRow(ctx, `SELECT generation,state,payload FROM identity.invitation_delivery WHERE invitation_id=$1`, id).Scan(&generation, &state, &payload); err != nil || generation != 2 || state != "queued" || payload == nil {
		t.Fatal("old completion overwrote resend", err, generation, state)
	}
	if _, err = i.Begin(ctx, old); err == nil {
		t.Fatal("old email still valid")
	}
	failure := &captureMail{fail: true}
	for n := 0; n < 5; n++ {
		if _, err = i.Store.DB.Exec(ctx, `UPDATE identity.invitation_delivery SET next_attempt=now()-interval '1 minute' WHERE invitation_id=$1`, id); err != nil {
			t.Fatal(err)
		}
		if _, err = i.DeliverOne(ctx, failure); err != nil {
			t.Fatal(err)
		}
	}
	if err = i.Store.DB.QueryRow(ctx, `SELECT state,payload FROM identity.invitation_delivery WHERE invitation_id=$1`, id).Scan(&state, &payload); err != nil || state != "failed" || payload != nil {
		t.Fatal("unbounded retry or secret retention", err, state)
	}
	if done, err := i.DeliverOne(ctx, failure); err != nil || done || failure.calls != 5 {
		t.Fatal("attempt bound", err)
	}
}

func TestInvitationIssuerSessionAndTenantSelection(t *testing.T) {
	i, tenant, raw := invitationFixture(t)
	ctx := context.Background()
	other := uuid.New()
	if _, err := i.Store.DB.Exec(ctx, `INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'Other',$2,'active',now(),now())`, other, "tenant_"+strings.ReplaceAll(other.String(), "-", "")); err != nil {
		t.Fatal(err)
	}
	user, _ := provision(t, i.Store, other, "multi", "platform_viewer")
	id := createInvite(t, i, raw, "multi@example.test")
	token := invitationToken(t, i, id)
	session, err := i.Accept(ctx, digest(token), recipient("multi@example.test", "multi"))
	if err != nil {
		t.Fatal(err)
	}
	current, err := i.Store.Resolve(ctx, session)
	if err != nil || current.TenantID != tenant || current.UserID != user {
		t.Fatal("invitation chose unrelated tenant or new identity", err)
	}
}
