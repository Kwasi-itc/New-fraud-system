package mysqlruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/observability"
	store "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/store/mysql"
)

func RunWorker(ctx context.Context, log *slog.Logger, m store.Maintenance, publisher, token string, ready func()) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for {
		run, cancel := context.WithTimeout(ctx, 30*time.Second)
		e := m.CleanUploads(run, 100)
		if e == nil {
			_, e = m.WakeSnoozed(run, 100)
		}
		if e == nil {
			_, e = m.AssignWaiting(run, 100)
		}
		if e == nil {
			ready()
		} else if ctx.Err() == nil {
			log.Error("case maintenance failed", observability.ErrorFields(e)...)
		}
		cancel()
		if publisher != "" {
			delivery, cancelDelivery := context.WithTimeout(ctx, 30*time.Second)
			for n := 0; n < 100 && delivery.Err() == nil; n++ {
				claim, cancel := context.WithTimeout(delivery, 5*time.Second)
				d, e := m.ClaimDelivery(claim)
				cancel()
				if e != nil {
					log.Error("case delivery claim failed", observability.ErrorFields(e)...)
					break
				}
				if d == nil {
					break
				}
				failure := publish(delivery, client, publisher, token, *d)
				finish, cancel := context.WithTimeout(ctx, 5*time.Second)
				e = m.FinishDelivery(finish, *d, failure)
				cancel()
				if e != nil {
					log.Error("case delivery completion failed", append([]any{"event_id", d.ID}, observability.ErrorFields(e)...)...)
					break
				}
			}
			cancelDelivery()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func publish(ctx context.Context, client *http.Client, url, token string, d store.Delivery) string {
	b, e := json.Marshal(d)
	if e != nil {
		return "payload encoding failed"
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if e != nil {
		return "publisher request invalid"
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", d.ID.String())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, e := client.Do(req)
	if e != nil {
		return "publisher unavailable"
	}
	defer resp.Body.Close()
	if _, e = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096)); e != nil {
		return "publisher response incomplete"
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "publisher rejected notification"
	}
	return ""
}
