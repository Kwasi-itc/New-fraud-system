package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"time"

	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
)

// WorkflowDispatchRequest implements the decision-v1 receipt wire contract.
// Its tenant is an assertion checked against the trusted integration principal.
type WorkflowDispatchRequest struct {
	Key               string          `json:"idempotency_key"`
	Kind              string          `json:"kind"`
	TenantID          string          `json:"tenant_id"`
	ResourceID        string          `json:"resource_id"`
	DomainAttempt     int             `json:"domain_attempt"`
	RedriveGeneration int             `json:"redrive_generation"`
	PayloadSHA256     string          `json:"payload_sha256"`
	Payload           json.RawMessage `json:"payload"`
}
type WorkflowDispatchReceipt struct {
	Key           string `json:"idempotency_key"`
	PayloadSHA256 string `json:"payload_sha256"`
	Outcome       string `json:"outcome"`
	Reference     string `json:"reference"`
}

func (r WorkflowDispatchRequest) validate() (uuid.UUID, uuid.UUID, error) {
	tenant, e1 := uuid.Parse(r.TenantID)
	id, e2 := uuid.Parse(r.ResourceID)
	if e1 != nil || e2 != nil || tenant == uuid.Nil || id == uuid.Nil || tenant.String() != r.TenantID || id.String() != r.ResourceID || r.Kind != "workflow_execution" || r.DomainAttempt < 1 || r.DomainAttempt > math.MaxInt32 || r.RedriveGeneration < 0 || r.RedriveGeneration > math.MaxInt32 || len(r.Payload) == 0 || len(r.Payload) > 1<<20 || !json.Valid(r.Payload) {
		return uuid.Nil, uuid.Nil, casepkg.Invalid("invalid workflow receipt identity")
	}
	keyJSON, err := json.Marshal(struct {
		Kind                             string
		TenantID, ResourceID             uuid.UUID
		DomainAttempt, RedriveGeneration int
	}{r.Kind, tenant, id, r.DomainAttempt, r.RedriveGeneration})
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	key := sha256.Sum256(keyJSON)
	hash := sha256.Sum256(r.Payload)
	if r.Key != "decision-v1-"+hex.EncodeToString(key[:]) || r.PayloadSHA256 != hex.EncodeToString(hash[:]) {
		return uuid.Nil, uuid.Nil, casepkg.Invalid("workflow receipt hash mismatch")
	}
	return tenant, id, nil
}
func receiptJSON(raw []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return casepkg.Invalid("invalid workflow receipt payload")
	}
	var trailing any
	if err := d.Decode(&trailing); err != io.EOF {
		return casepkg.Invalid("invalid workflow receipt payload")
	}
	return nil
}
func (r WorkflowDispatchRequest) input() (WorkflowActionInput, error) {
	var body struct {
		ExecutionID      uuid.UUID       `json:"execution_id"`
		TenantID         uuid.UUID       `json:"tenant_id"`
		DecisionID       uuid.UUID       `json:"decision_id"`
		ScenarioID       *uuid.UUID      `json:"scenario_id"`
		WorkflowID       *uuid.UUID      `json:"workflow_id"`
		WorkflowRuleID   *uuid.UUID      `json:"workflow_rule_id"`
		WorkflowActionID *uuid.UUID      `json:"workflow_action_id"`
		ActionType       string          `json:"action_type"`
		ActionConfig     json.RawMessage `json:"action_config"`
		CreatedAt        time.Time       `json:"created_at"`
	}
	if err := receiptJSON(r.Payload, &body); err != nil {
		return WorkflowActionInput{}, err
	}
	if body.ExecutionID.String() != r.ResourceID || body.TenantID.String() != r.TenantID {
		return WorkflowActionInput{}, casepkg.Invalid("workflow payload identity mismatch")
	}
	var config WorkflowActionConfig
	if err := receiptJSON(body.ActionConfig, &config); err != nil {
		return WorkflowActionInput{}, err
	}
	return WorkflowActionInput{WorkflowExecutionID: body.ExecutionID, TenantID: body.TenantID, DecisionID: body.DecisionID, ScenarioID: body.ScenarioID, ActionType: body.ActionType, ActionConfig: config}, nil
}
func (r WorkflowDispatchRequest) receiptSource() string {
	return fmt.Sprintf("workflow.receipt.v1:%d:%d", r.DomainAttempt, r.RedriveGeneration)
}
func (r WorkflowDispatchRequest) result(id uuid.UUID) WorkflowDispatchReceipt {
	out := WorkflowDispatchReceipt{Key: r.Key, PayloadSHA256: r.PayloadSHA256, Outcome: "not_found"}
	if id != uuid.Nil {
		out.Outcome = "accepted"
		out.Reference = id.String()
	}
	return out
}

// DeliverWorkflowReceipt reuses the actual case service mutation rules. Full
// attempt receipts and the execution-wide body binding commit with the existing
// case/link/tag/audit/outbox writes in one owner transaction. Replays of any
// accepted attempt preserve the original case, including after closure/transfer.
func (s CaseService) DeliverWorkflowReceipt(ctx context.Context, r WorkflowDispatchRequest) (WorkflowDispatchReceipt, error) {
	owner := WorkflowReceiptService{store: caseWorkflowReceiptStore{s}, clock: s.clock}
	return owner.DeliverWorkflowReceipt(ctx, r)
}

func (s CaseService) ReconcileWorkflowReceipt(ctx context.Context, r WorkflowDispatchRequest) (WorkflowDispatchReceipt, error) {
	owner := WorkflowReceiptService{store: caseWorkflowReceiptStore{s}, clock: s.clock}
	return owner.ReconcileWorkflowReceipt(ctx, r)
}
