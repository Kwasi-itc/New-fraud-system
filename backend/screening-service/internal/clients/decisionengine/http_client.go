package decisionengine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
)

type HTTPClient struct {
	baseURL   string
	authMode  string
	authToken string
	client    *http.Client
}

func NewHTTPClient(baseURL, authMode, authToken string, timeout time.Duration) HTTPClient {
	return HTTPClient{
		baseURL:   strings.TrimRight(baseURL, "/"),
		authMode:  strings.ToLower(strings.TrimSpace(authMode)),
		authToken: authToken,
		client:    &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
}

func (c HTTPClient) PublishScreeningStatusChanged(ctx context.Context, command ports.ScreeningStatusChangedCommand) error {
	if c.baseURL == "" {
		return nil
	}

	if c.authMode != "token" || len(strings.TrimSpace(c.authToken)) < 32 {
		return fmt.Errorf("screening callback credential is unavailable")
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
	if c.authMode == "token" && c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
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
