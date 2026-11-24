package caseclient

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

func (c HTTPClient) PublishScreeningReviewed(ctx context.Context, command ports.ScreeningReviewedCommand) error {
	return c.post(ctx, "/v1/screening-events/reviewed", command)
}

func (c HTTPClient) PublishScreeningRequiresReview(ctx context.Context, command ports.ScreeningRequiresReviewCommand) error {
	return c.post(ctx, "/v1/screening-events/requires-review", command)
}

func (c HTTPClient) PublishScreeningEvidenceUploaded(ctx context.Context, command ports.ScreeningEvidenceUploadedCommand) error {
	return c.post(ctx, "/v1/screening-events/evidence-uploaded", command)
}

func (c HTTPClient) post(ctx context.Context, path string, payload any) error {
	if c.baseURL == "" {
		return fmt.Errorf("case integration URL is required")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal case command: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create case request: invalid request")
	}
	req.Header.Set("Content-Type", "application/json")
	var envelope struct {
		TenantID uuid.UUID `json:"tenant_id"`
	}
	if err = json.Unmarshal(body, &envelope); err != nil {
		return err
	}
	if err = platformauth.AuthorizeServiceRequest(req, envelope.TenantID, "urn:fraud:case-manager-service", []string{"cases:events-write"}, c.Signer); err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("execute case request: transport failure")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status from case-service: %d", resp.StatusCode)
	}
	return nil
}

func (c HTTPClient) WithSigner(signer *platformauth.Signer) HTTPClient { c.Signer = signer; return c }
