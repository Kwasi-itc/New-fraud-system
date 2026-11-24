package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Command struct {
	OperationID     uuid.UUID `json:"operation_id"`
	ResourceID      string    `json:"resource_id"`
	ExpectedVersion string    `json:"expected_version"`
	Action          string    `json:"action"`
	Findings        string    `json:"findings"`
}
type State struct {
	Status         string   `json:"status"`
	Version        string   `json:"version"`
	Eligible       bool     `json:"eligible"`
	MachineOutcome string   `json:"machine_outcome,omitempty"`
	Actions        []string `json:"actions"`
}
type Apply func(context.Context, pgx.Tx, *Command) (State, error)
type caseReceipts struct {
	principal               Principal
	tenant, caseID          uuid.UUID
	resource, owner         string
	command                 *Command
	before, out             State
	caseStatus, inboxStatus string
}

func (s *caseReceipts) Authorize(ctx context.Context, tx pgx.Tx, p Principal) error {
	var allowed bool
	if err := tx.QueryRow(ctx, `SELECT allowed,case_status,inbox_status FROM case_manager.authorize_case_access_v1($1,$2,$3,$4,$5,$6,$7,$8)`, s.tenant, s.caseID, p.Subject, s.owner, s.resource, p.SessionID, p.AuthzVersion, true).Scan(&allowed, &s.caseStatus, &s.inboxStatus); err != nil {
		return err
	}
	if !allowed {
		return Forbidden()
	}
	return nil
}
func (s *caseReceipts) Load(ctx context.Context, tx pgx.Tx, tenant, operation uuid.UUID) (string, json.RawMessage, bool, error) {
	var hash string
	var result json.RawMessage
	err := tx.QueryRow(ctx, `SELECT payload_hash,result FROM `+s.owner+`.investigator_reviews WHERE tenant_id=$1 AND operation_id=$2`, tenant, operation).Scan(&hash, &result)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, false, nil
	}
	return hash, result, err == nil, err
}
func (s *caseReceipts) Record(ctx context.Context, tx pgx.Tx, tenant, operation uuid.UUID, hash string, result json.RawMessage) error {
	if _, err := tx.Exec(ctx, `INSERT INTO `+s.owner+`.investigator_reviews(tenant_id,operation_id,case_id,resource_id,actor_id,previous_status,status,findings,payload_hash,result) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, tenant, operation, s.caseID, s.resource, s.principal.Subject, s.before.Status, s.out.Status, s.command.Findings, hash, result); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `SELECT case_manager.record_investigator_review($1,$2,$3)`, tenant, s.owner, operation)
	return err
}

// Run is the case-review adapter over the generic hardened-action pipeline.
// Owner callbacks retain their mutation rules and execute in the same transaction.
func Run(ctx context.Context, db *pgxpool.Pool, p Principal, tenant, caseID uuid.UUID, resource, owner string, cmd *Command, apply Apply) (State, error) {
	if !p.IsHuman() || !p.Can("cases:review") || p.TenantID != tenant || tenant == uuid.Nil || caseID == uuid.Nil {
		return State{}, Forbidden()
	}
	if resource == "" || len(resource) > 1024 || owner != "core" && owner != "screening" {
		return State{}, Invalid("A supported owner and resource are required.")
	}
	receipts := &caseReceipts{principal: p, tenant: tenant, caseID: caseID, resource: resource, owner: owner, command: cmd}
	if cmd == nil {
		tx, err := db.Begin(ctx)
		if err != nil {
			return State{}, err
		}
		defer rollback(ctx, tx)
		if err = CheckCapability(ctx, tx, p, "cases:review", true); err != nil {
			return State{}, err
		}
		if err = receipts.Authorize(ctx, tx, p); err != nil {
			return State{}, err
		}
		out, err := apply(ctx, tx, nil)
		if err != nil {
			return State{}, err
		}
		out.Eligible = out.Eligible && receipts.caseStatus != "closed" && receipts.inboxStatus == "active"
		return out, tx.Commit(ctx)
	}
	cmd.Findings = strings.TrimSpace(cmd.Findings)
	if cmd.OperationID == uuid.Nil || cmd.ResourceID != resource || len(cmd.Findings) == 0 || len(cmd.Findings) > 10000 || cmd.ExpectedVersion == "" {
		return State{}, Invalid("Operation, matching resource, expected version and findings are required.")
	}
	payload, err := json.Marshal(cmd)
	if err != nil {
		return State{}, err
	}
	// Existing immutable receipts retain their historical digest. This permits
	// exact receipt replay after the auth migration without accepting legacy tokens.
	legacy, err := json.Marshal(struct {
		Command *Command
		Actor   string
		Case    uuid.UUID
	}{cmd, p.Subject, caseID})
	if err != nil {
		return State{}, err
	}
	digest := sha256.Sum256(legacy)
	result, err := ExecuteHardenedAction(ctx, db, HardenedAction{Principal: p, Capability: "cases:review", OperationID: cmd.OperationID, Domain: owner, Resource: resource, Payload: payload, PreviousPayloadHash: hex.EncodeToString(digest[:]), Receipts: receipts}, func(ctx context.Context, tx pgx.Tx) (json.RawMessage, error) {
		if receipts.caseStatus == "closed" || receipts.inboxStatus != "active" {
			return nil, Invalid("Reopen the case in an active inbox before reviewing.")
		}
		before, err := apply(ctx, tx, nil)
		if err != nil {
			return nil, err
		}
		receipts.before = before
		if !before.Eligible || before.Version != cmd.ExpectedVersion {
			return nil, Conflict()
		}
		if !slices.Contains(before.Actions, cmd.Action) {
			return nil, Invalid("Action is not available.")
		}
		out, err := apply(ctx, tx, cmd)
		if err != nil {
			return nil, err
		}
		out.Eligible = false
		receipts.out = out
		return json.Marshal(out)
	})
	if err != nil {
		return State{}, err
	}
	var out State
	err = json.Unmarshal(result, &out)
	return out, err
}
