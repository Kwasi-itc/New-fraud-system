package decisionengine

import (
	"bytes"
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

func (c HTTPClient) PublishScreeningStatusChanged(ctx context.Context, command ports.ScreeningStatusChangedCommand) error {
	if c.baseURL == "" {
		return fmt.Errorf("decision engine callback URL is required")
	}

	body, err := json.Marshal(command)
	if err != nil {
		return fmt.Errorf("marshal decision engine callback: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/internal/screening-status-updates", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create decision engine callback: invalid request")
	}
	req.Header.Set("Content-Type", "application/json")
	tenant, authErr := uuid.Parse(command.TenantID)
	if authErr != nil {
		return authErr
	}
	if authErr = platformauth.AuthorizeServiceRequest(req, tenant, "urn:fraud:decision-engine-service", []string{"screening:callback"}, c.Signer); authErr != nil {
		return authErr
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("execute decision engine callback: transport failure")
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("decision engine returned status %d", resp.StatusCode)
	}
	return nil
}

func (c HTTPClient) WithSigner(signer *platformauth.Signer) HTTPClient { c.Signer = signer; return c }
