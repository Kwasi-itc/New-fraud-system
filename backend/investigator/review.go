package investigator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
)

type Principal struct {
	Subject      string
	TenantID     uuid.UUID
	Admin        bool
	SessionID    uuid.UUID
	AuthzVersion int64
}
type Error struct {
	Code    int
	Message string
}

func (e *Error) Error() string     { return e.Message }
func Invalid(message string) error { return &Error{400, message} }
func Conflict() error {
	return &Error{409, "Review changed or command conflicts; refresh before retrying."}
}
func Forbidden() error { return &Error{403, "Current case/resource access is required."} }
func HTTPError(err error) (int, string) {
	var e *Error
	if errors.As(err, &e) {
		return e.Code, e.Message
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return 409, "Review already recorded; refresh before retrying."
	}
	return 500, "Review could not be completed. Retry with the same operation ID."
}

// Unconfigured owner endpoints fail closed; partial/bad config fails startup.
func FromEnvironment() (*Verifier, error) {
	path, issuer, audience := os.Getenv("USER_JWT_KEYS_FILE"), os.Getenv("USER_JWT_ISSUER"), os.Getenv("USER_JWT_AUDIENCE")
	if path == "" && issuer == "" && audience == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var values map[string]string
	if err = json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	keys := map[string][]byte{}
	for id, key := range values {
		keys[id] = []byte(key)
	}
	return NewVerifier(keys, issuer, audience)
}

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

// Run atomically checks case authority, invokes owner logic, stores an owner
// receipt and calls the case-owned history function. No network IO is permitted.
func Run(ctx context.Context, db *pgxpool.Pool, p Principal, tenant, caseID uuid.UUID, resource, owner string, cmd *Command, apply Apply) (State, error) {
	if p.Subject == "" || p.TenantID != tenant || tenant == uuid.Nil || caseID == uuid.Nil {
		return State{}, Forbidden()
	}
	if resource == "" || len(resource) > 1024 {
		return State{}, Invalid("Resource is required.")
	}
	if owner != "core" && owner != "screening" {
		return State{}, Invalid("Unsupported review owner.")
	}
	if cmd != nil {
		cmd.Findings = strings.TrimSpace(cmd.Findings)
		if cmd.OperationID == uuid.Nil || cmd.Findings == "" || len(cmd.Findings) > 10000 || cmd.ExpectedVersion == "" {
			return State{}, Invalid("Operation, expected version and findings (1–10000 UTF-8 bytes) are required.")
		}
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return State{}, err
	}
	defer tx.Rollback(ctx)
	if err = CheckIdentity(ctx, tx, p, true); err != nil {
		return State{}, err
	}
	if cmd != nil {
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, tenant.String()+":"+owner+":"+cmd.OperationID.String()); err != nil {
			return State{}, err
		}
	}
	var inbox uuid.UUID
	var status, inboxStatus string
	err = tx.QueryRow(ctx, `SELECT inbox_id,status FROM case_manager.cases WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, caseID).Scan(&inbox, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return State{}, Forbidden()
	}
	if err != nil {
		return State{}, err
	}
	err = tx.QueryRow(ctx, `SELECT status FROM case_manager.inboxes WHERE tenant_id=$1 AND id=$2 FOR SHARE`, tenant, inbox).Scan(&inboxStatus)
	if err != nil {
		return State{}, err
	}
	var member bool
	if !p.Admin {
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM case_manager.inbox_users WHERE tenant_id=$1 AND inbox_id=$2 AND user_id=$3)`, tenant, inbox, p.Subject).Scan(&member)
		if err != nil {
			return State{}, err
		}
		if !member {
			return State{}, Forbidden()
		}
	}
	var linked bool
	if owner == "core" {
		id, e := uuid.Parse(resource)
		if e != nil {
			return State{}, Invalid("Invalid decision ID.")
		}
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM case_manager.case_decisions WHERE tenant_id=$1 AND case_id=$2 AND decision_id=$3)`, tenant, caseID, id).Scan(&linked)
	} else {
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM case_manager.case_screenings l JOIN screening.screening_matches m ON m.tenant_id=l.tenant_id AND m.screening_id=l.screening_id AND m.id=l.match_id WHERE l.tenant_id=$1 AND l.case_id=$2 AND l.match_id=$3)`, tenant, caseID, resource).Scan(&linked)
	}
	if err != nil {
		return State{}, err
	}
	if !linked {
		return State{}, Forbidden()
	}
	table := owner + ".investigator_reviews"
	var hash string
	if cmd != nil {
		raw, _ := json.Marshal(struct {
			Command *Command
			Actor   string
			Case    uuid.UUID
		}{cmd, p.Subject, caseID})
		sum := sha256.Sum256(raw)
		hash = hex.EncodeToString(sum[:])
		var previous string
		var result []byte
		err = tx.QueryRow(ctx, `SELECT payload_hash,result FROM `+table+` WHERE tenant_id=$1 AND operation_id=$2`, tenant, cmd.OperationID).Scan(&previous, &result)
		if err == nil {
			if previous != hash {
				return State{}, Conflict()
			}
			var out State
			if err = json.Unmarshal(result, &out); err != nil {
				return State{}, err
			}
			return out, tx.Commit(ctx)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return State{}, err
		}
		if status == "closed" || inboxStatus != "active" {
			return State{}, Invalid("Reopen the case in an active inbox before reviewing.")
		}
	}
	before, err := apply(ctx, tx, nil)
	if err != nil {
		return State{}, err
	}
	if cmd == nil {
		before.Eligible = before.Eligible && status != "closed" && inboxStatus == "active"
		return before, tx.Commit(ctx)
	}
	if !before.Eligible || before.Version != cmd.ExpectedVersion {
		return State{}, Conflict()
	}
	allowed := false
	for _, action := range before.Actions {
		if action == cmd.Action {
			allowed = true
		}
	}
	if !allowed {
		return State{}, Invalid("Action is not available.")
	}
	out, err := apply(ctx, tx, cmd)
	if err != nil {
		return State{}, err
	}
	out.Eligible = false
	result, err := json.Marshal(out)
	if err != nil {
		return State{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO `+table+`(tenant_id,operation_id,case_id,resource_id,actor_id,previous_status,status,findings,payload_hash,result) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, tenant, cmd.OperationID, caseID, resource, p.Subject, before.Status, out.Status, cmd.Findings, hash, result)
	if err != nil {
		return State{}, err
	}
	if _, err = tx.Exec(ctx, `SELECT case_manager.record_investigator_review($1,$2,$3)`, tenant, owner, cmd.OperationID); err != nil {
		return State{}, err
	}
	return out, tx.Commit(ctx)
}
