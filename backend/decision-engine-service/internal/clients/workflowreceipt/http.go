// Package workflowreceipt binds case workflow dispatch to the case owner's v1
// receipt API. Arbitrary action destinations and external events are excluded.
package workflowreceipt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

var ErrUnavailable = errors.New("case workflow receipt unavailable; reconcile original identity")

type Config struct {
	BaseURL, Token string
	Timeout        time.Duration
	AllowHTTP      bool
}
type HTTP struct {
	base, token string
	client      *http.Client
}

var _ ports.ReceiptDispatcher = (*HTTP)(nil)

func New(c Config) (*HTTP, error) {
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Host == "" || u.User != nil || strings.ContainsAny(c.BaseURL, "?#") || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || u.Path != "" && u.Path != "/" || u.Scheme != "https" && !(u.Scheme == "http" && c.AllowHTTP) || len(c.Token) < 32 || strings.TrimSpace(c.Token) != c.Token || strings.ContainsAny(c.Token, "\r\n") || c.Timeout <= 0 || c.Timeout > time.Minute {
		return nil, ErrUnavailable
	}
	return &HTTP{strings.TrimRight(c.BaseURL, "/"), c.Token, &http.Client{Timeout: c.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *HTTP) Deliver(ctx context.Context, r ports.DispatchRequest) (ports.DispatchReceipt, error) {
	return c.call(ctx, r, false)
}
func (c *HTTP) Reconcile(ctx context.Context, r ports.DispatchRequest) (ports.DispatchReceipt, error) {
	return c.call(ctx, r, true)
}
func (c *HTTP) call(ctx context.Context, r ports.DispatchRequest, reconcile bool) (ports.DispatchReceipt, error) {
	if c == nil || c.client == nil || r.Kind != ports.JobWorkflow || r.Key == "" || r.PayloadSHA256 == "" || len(r.Payload) == 0 || len(r.Payload) > 1<<20 {
		return ports.DispatchReceipt{}, ErrUnavailable
	}
	var body struct {
		ActionType workflow.ActionType `json:"action_type"`
	}
	if json.Unmarshal(r.Payload, &body) != nil || !workflow.IsCaseAction(body.ActionType) {
		return ports.DispatchReceipt{}, ErrUnavailable
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return ports.DispatchReceipt{}, ErrUnavailable
	}
	operation := "deliver"
	if reconcile {
		operation = "reconcile"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/internal/v1/workflow-receipts/"+operation, bytes.NewReader(raw))
	if err != nil {
		return ports.DispatchReceipt{}, ErrUnavailable
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+c.token)
	response, err := c.client.Do(request)
	if err != nil {
		return ports.DispatchReceipt{}, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ports.DispatchReceipt{}, ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 16385))
	if err != nil || len(data) > 16384 {
		return ports.DispatchReceipt{}, ErrUnavailable
	}
	var out ports.DispatchReceipt
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&out) != nil {
		return ports.DispatchReceipt{}, ErrUnavailable
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || out.Key != r.Key || out.PayloadSHA256 != r.PayloadSHA256 {
		return ports.DispatchReceipt{}, ErrUnavailable
	}
	switch out.Outcome {
	case ports.DispatchAccepted, ports.DispatchRejected:
		if out.Reference == "" || len(out.Reference) > 512 || !utf8.ValidString(out.Reference) || strings.ContainsRune(out.Reference, 0) {
			return ports.DispatchReceipt{}, ErrUnavailable
		}
	case ports.DispatchNotFound, ports.DispatchUnknown:
		if !reconcile || out.Reference != "" {
			return ports.DispatchReceipt{}, ErrUnavailable
		}
	default:
		return ports.DispatchReceipt{}, ErrUnavailable
	}
	return out, nil
}
