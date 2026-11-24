package inbox

import (
	"context"
	"encoding/json"
	"fmt"
	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/google/uuid"
	"net/http"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
)

type HTTPClient struct {
	Signer  *platformauth.Signer
	baseURL string

	client *http.Client
}

func NewHTTPClient(baseURL string, timeout time.Duration) HTTPClient {
	return HTTPClient{
		baseURL: strings.TrimRight(baseURL, "/"),

		client: &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
}

func (c HTTPClient) GetInbox(ctx context.Context, tenantID, inboxID string) (ports.Inbox, error) {
	if c.baseURL == "" {
		return ports.Inbox{}, fmt.Errorf("inbox integration URL is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/internal/v1/tenants/%s/inboxes/%s", c.baseURL, tenantID, inboxID), nil)
	if err != nil {
		return ports.Inbox{}, fmt.Errorf("create inbox request: %w", err)
	}
	tenant, authErr := uuid.Parse(tenantID)
	if authErr != nil {
		return ports.Inbox{}, authErr
	}
	if authErr = platformauth.AuthorizeServiceRequest(req, tenant, "urn:fraud:case-manager-service", []string{"inboxes:read"}, c.Signer); authErr != nil {
		return ports.Inbox{}, authErr
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return ports.Inbox{}, fmt.Errorf("execute inbox request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ports.Inbox{}, fmt.Errorf("unexpected status from inbox-service: %d", resp.StatusCode)
	}
	var payload struct {
		Inbox ports.Inbox `json:"inbox"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ports.Inbox{}, fmt.Errorf("decode inbox response: %w", err)
	}
	return payload.Inbox, nil
}

func (c HTTPClient) WithSigner(signer *platformauth.Signer) HTTPClient { c.Signer = signer; return c }
