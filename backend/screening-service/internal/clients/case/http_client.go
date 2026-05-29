package caseclient

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
	baseURL string
	client  *http.Client
}

func NewHTTPClient(baseURL string, timeout time.Duration) HTTPClient {
	return HTTPClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: timeout},
	}
}

func (c HTTPClient) PublishScreeningReviewed(ctx context.Context, command ports.ScreeningReviewedCommand) error {
	return c.post(ctx, "/v1/screening-events/reviewed", command)
}

func (c HTTPClient) PublishScreeningEvidenceUploaded(ctx context.Context, command ports.ScreeningEvidenceUploadedCommand) error {
	return c.post(ctx, "/v1/screening-events/evidence-uploaded", command)
}

func (c HTTPClient) post(ctx context.Context, path string, payload any) error {
	if c.baseURL == "" {
		return nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal case command: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create case request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("execute case request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("unexpected status from case-service: %d", resp.StatusCode)
	}
	return nil
}
