package dispatch

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

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
)

type HTTPClient struct {
	Signer *platformauth.Signer

	client *http.Client

	workflowURL        string
	screeningURL       string
	scoringURL         string
	outboxPublisherURL string
}

func NewHTTPClient(timeout time.Duration, workflowURL, screeningURL, scoringURL, outboxPublisherURL string) HTTPClient {
	return HTTPClient{
		client: &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},

		workflowURL:        strings.TrimRight(workflowURL, "/"),
		screeningURL:       strings.TrimRight(screeningURL, "/"),
		scoringURL:         strings.TrimRight(scoringURL, "/"),
		outboxPublisherURL: strings.TrimRight(outboxPublisherURL, "/"),
	}
}

func (c HTTPClient) DispatchWorkflowExecution(ctx context.Context, item workflow.Execution) error {
	if err := workflow.ValidateActionConfig(item.ActionType, item.ActionConfig); err != nil {
		return err
	}
	url := c.workflowURL

	if !workflow.IsCaseAction(item.ActionType) {
		url = workflowURLFromConfig(item.ActionConfig)
		// Action-supplied destinations receive no internal identity.
	}
	if url == "" {
		return fmt.Errorf("workflow dispatcher URL is not configured")
	}
	payload := map[string]any{
		"workflow_execution_id": item.ID,
		"tenant_id":             item.TenantID,
		"workflow_id":           item.WorkflowID,
		"workflow_rule_id":      item.WorkflowRuleID,
		"workflow_action_id":    item.WorkflowActionID,
		"decision_id":           item.DecisionID,
		"scenario_id":           item.ScenarioID,
		"action_type":           item.ActionType,
		"action_config":         json.RawMessage(item.ActionConfig),
		"created_at":            item.CreatedAt,
	}
	if workflow.IsCaseAction(item.ActionType) {
		body, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		return c.postInternal(ctx, url, body, item.TenantID, "urn:fraud:case-manager-service", "workflows:execute")
	}
	return c.postJSON(ctx, url, payload)
}

func (c HTTPClient) SendScreeningExecution(ctx context.Context, item screening.Execution) error {
	if c.screeningURL == "" {
		return fmt.Errorf("screening service URL is not configured")
	}
	if err := c.postInternal(ctx, c.screeningURL+"/internal/v1/tenants/"+item.TenantID+"/decision-screenings", item.RequestJSON, item.TenantID, "urn:fraud:screening-service", "screening:intake"); err != nil {
		// River records returned errors. Do not persist internal endpoints,
		// transport details, credentials or provider response bodies in job errors.
		return fmt.Errorf("screening dispatch failed")
	}
	return nil
}

func (c HTTPClient) SendScoringRequest(ctx context.Context, item scoring.Request) error {
	if c.scoringURL == "" {
		return fmt.Errorf("scoring provider URL is not configured")
	}
	return c.postRawJSON(ctx, c.scoringURL, item.RequestJSON)
}

func (c HTTPClient) PublishOutboxEvent(ctx context.Context, item integration.OutboxEvent) error {
	if c.outboxPublisherURL == "" {
		return fmt.Errorf("outbox publisher URL is not configured")
	}
	return c.postJSON(ctx, c.outboxPublisherURL, map[string]any{
		"event_id":         item.ID,
		"tenant_id":        item.TenantID,
		"aggregate_type":   item.AggregateType,
		"aggregate_id":     item.AggregateID,
		"event_type":       item.EventType,
		"payload":          json.RawMessage(item.Payload),
		"status":           item.Status,
		"event_created_at": item.CreatedAt,
	})
}

func (c HTTPClient) postJSON(ctx context.Context, url string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return c.postRawJSON(ctx, url, body)
}

func (c HTTPClient) postRawJSON(ctx context.Context, url string, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("dispatch request to %s returned status %d", url, resp.StatusCode)
	}
	return nil
}

func workflowURLFromConfig(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var cfg struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return ""
	}
	return strings.TrimSpace(cfg.URL)
}

func (c HTTPClient) postInternal(ctx context.Context, url string, body []byte, tenantID, audience, capability string) error {
	tenant, err := uuid.Parse(tenantID)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if err = platformauth.AuthorizeServiceRequest(req, tenant, audience, []string{capability}, c.Signer); err != nil {
		return err
	}
	response, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("internal delivery transport failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("internal delivery returned status %d", response.StatusCode)
	}
	return nil
}

func (c HTTPClient) WithSigner(signer *platformauth.Signer) HTTPClient { c.Signer = signer; return c }
