package decisionengine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
)

type HTTPClient struct {
	baseURL string
	client  *http.Client
}

func NewHTTPClient(baseURL string, timeout time.Duration) HTTPClient {
	return HTTPClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: timeout},
	}
}

func (c HTTPClient) PublishScreeningStatusChanged(ctx context.Context, command ports.ScreeningStatusChangedCommand) error {
	if c.baseURL == "" {
		return nil
	}

	body, err := json.Marshal(command)
	if err != nil {
		return fmt.Errorf("marshal decision engine callback: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/internal/screening-status-updates", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create decision engine callback: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("execute decision engine callback: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read decision engine callback response: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("decision engine returned status %d: %s", resp.StatusCode, string(raw))
	}
	return nil
}
