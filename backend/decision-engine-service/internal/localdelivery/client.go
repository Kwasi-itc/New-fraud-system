package localdelivery

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/google/uuid"
	"io"
	"net/http"
	"time"
)

type Client struct {
	base, token string
	tenant      uuid.UUID
	kind        ports.JobKind
	http        *http.Client
	multiTenant bool
}

func NewClient(base, token string, tenant uuid.UUID, kind ports.JobKind) (*Client, error) {
	if base != BaseURL || len(token) != 64 || tenant == uuid.Nil {
		return nil, ErrRequest
	}
	return &Client{base: base, token: token, tenant: tenant, kind: kind, http: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// The authenticated development worker may dispatch for any registered tenant;
// the receiver still validates that tenant inside each owner transaction.
func NewMultiTenantClient(base, token string, kind ports.JobKind) (*Client, error) {
	if base != BaseURL || len(token) != 64 {
		return nil, ErrRequest
	}
	return &Client{base: base, token: token, kind: kind, multiTenant: true, http: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *Client) Deliver(ctx context.Context, r ports.DispatchRequest) (ports.DispatchReceipt, error) {
	return c.call(ctx, r, "deliver")
}
func (c *Client) Reconcile(ctx context.Context, r ports.DispatchRequest) (ports.DispatchReceipt, error) {
	return c.call(ctx, r, "reconcile")
}
func (c *Client) call(ctx context.Context, r ports.DispatchRequest, op string) (out ports.DispatchReceipt, err error) {
	if c == nil {
		return out, ErrRequest
	}
	tenant := c.tenant
	if c.multiTenant {
		var e error
		tenant, e = uuid.Parse(r.TenantID)
		if e != nil {
			return out, ErrRequest
		}
	}
	if r.Kind != c.kind || Validate(r, tenant) != nil {
		return out, ErrRequest
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return out, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/internal/local-deliveries/"+op, bytes.NewReader(raw))
	if err != nil {
		return out, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return out, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return out, ErrRequest
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 16385))
	if err != nil || len(body) > 16384 {
		return out, ErrRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&out) != nil {
		return out, ErrRequest
	}
	var tail any
	if decoder.Decode(&tail) != io.EOF {
		return out, ErrRequest
	}
	if err := mysql.ValidateDispatchReceipt(r, out); err != nil {
		return ports.DispatchReceipt{}, err
	}
	return out, nil
}
