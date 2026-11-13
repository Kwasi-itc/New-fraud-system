// Package mysqlreview owns case-linked review receipts and their case history.
// Decision outcomes remain read-only. Identity and case access are checked on
// the caller transaction before any resource or receipt is mutated.
package mysqlreview

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/mysqlidentity"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/mysqlpublication"
	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"strings"
)

type Store struct {
	db               *sql.DB
	database, schema string
	session          mysqlidentity.Session
}

func New(db *sql.DB, database string, session mysqlidentity.Session) (*Store, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if db == nil || session.ActorID == uuid.Nil || session.SessionID == uuid.Nil || session.TenantID == uuid.Nil {
		return nil, identity.Forbidden()
	}
	return &Store{db, database, s, session}, nil
}
func (s *Store) Matches(db *sql.DB, database string, tenant uuid.UUID) bool {
	return s != nil && s.db == db && s.database == database && s.session.TenantID == tenant
}
func (s *Store) authorize(ctx context.Context, tx *sql.Tx, write bool) error {
	action := "decision.workflow.read"
	if write {
		action = "decision.workflow.author"
	}
	var allowed bool
	if err := tx.QueryRowContext(ctx, "SELECT "+s.schema+".identity_authorize_decision_v1(?,?,?,?,?,?)", mysqlstore.UUID(s.session.ActorID), mysqlstore.UUID(s.session.TenantID), mysqlstore.UUID(s.session.SessionID), s.session.MembershipVersion, action, write).Scan(&allowed); err != nil {
		return err
	}
	if !allowed || write && s.session.ReadOnly {
		return identity.Forbidden()
	}
	return nil
}
func state(outcome, status string) identity.State {
	v := identity.State{Status: "pending", Version: "0", MachineOutcome: outcome, Eligible: outcome == "review" || outcome == "block_and_review", Actions: []string{"approve", "decline"}}
	if status != "" {
		v.Status, v.Version, v.Eligible = status, "1", false
	} else if !v.Eligible {
		v.Status = "not_applicable"
	}
	return v
}

func (s *Store) Review(ctx context.Context, caseID, decision uuid.UUID, command *identity.Command) (out identity.State, err error) {
	if s == nil || caseID == uuid.Nil || decision == uuid.Nil {
		return out, identity.Forbidden()
	}
	if command != nil {
		command.Findings = strings.TrimSpace(command.Findings)
		if command.OperationID == uuid.Nil || command.ResourceID != decision.String() || command.ExpectedVersion == "" || command.Findings == "" || len(command.Findings) > 10000 {
			return out, identity.Invalid("Operation, resource, expected version and findings are required.")
		}
	}
	err = mysqlstore.Mutation(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.authorize(ctx, tx, command != nil); err != nil {
			return err
		}
		tenant := mysqlstore.UUID(s.session.TenantID)
		var status string
		var inbox mysqlstore.UUID
		var active bool
		if err := tx.QueryRowContext(ctx, "SELECT c.status,c.inbox_id,i.active FROM "+s.schema+".case_manager_cases c JOIN "+s.schema+".case_manager_inboxes i ON i.tenant_id=c.tenant_id AND i.id=c.inbox_id WHERE c.tenant_id=? AND c.id=? FOR SHARE OF c,i", tenant, mysqlstore.UUID(caseID)).Scan(&status, &inbox, &active); err != nil {
			return err
		}
		var access bool
		if err := tx.QueryRowContext(ctx, "SELECT "+s.schema+".case_manager_authorize_decision_inboxes_v1(?,?,?,?,JSON_ARRAY(BIN_TO_UUID(?)),FALSE,TRUE)", mysqlstore.UUID(s.session.ActorID), tenant, mysqlstore.UUID(s.session.SessionID), s.session.MembershipVersion, inbox).Scan(&access); err != nil {
			return err
		}
		if !access {
			return identity.Forbidden()
		}
		var linked bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+s.schema+".case_manager_case_decisions WHERE tenant_id=? AND case_id=? AND decision_id=?)", tenant, mysqlstore.UUID(caseID), mysqlstore.UUID(decision)).Scan(&linked); err != nil {
			return err
		}
		if !linked {
			return identity.Forbidden()
		}
		// Use the case owner's resource lock; the decision remains read-only.
		// Every operation ID for this decision serializes on the same key.
		lock := sha256.Sum256([]byte("decision-review:" + decision.String()))
		if _, err := tx.ExecContext(ctx, "INSERT INTO "+s.schema+".case_manager_workflow_locks(tenant_id,lock_hash) VALUES(?,?) ON DUPLICATE KEY UPDATE lock_hash=lock_hash", tenant, lock[:]); err != nil {
			return err
		}
		var outcome string
		if err := tx.QueryRowContext(ctx, "SELECT outcome FROM "+s.schema+".decision_engine_decisions WHERE tenant_id=? AND id=?", tenant, mysqlstore.UUID(decision)).Scan(&outcome); err != nil {
			return err
		}
		var previous string
		err := tx.QueryRowContext(ctx, "SELECT status FROM "+s.schema+".case_manager_resource_reviews WHERE tenant_id=? AND resource_id=?", tenant, mysqlstore.UUID(decision)).Scan(&previous)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		before := state(outcome, previous)
		before.Eligible = before.Eligible && active && status != "closed"
		if before.Eligible {
			reason, err := mysqlpublication.DecisionReviewReason(ctx, tx, s.database, s.session.TenantID, decision)
			if err != nil {
				return err
			}
			if reason != "" {
				before.Actions = []string{"decline"}
				before.DisabledActions = map[string]string{"approve": reason}
			}
		}
		if command == nil {
			out = before
			return nil
		}
		raw, err := json.Marshal(struct {
			Command     *identity.Command
			Actor, Case uuid.UUID
		}{command, s.session.ActorID, caseID})
		if err != nil {
			return err
		}
		digest := sha256.Sum256(raw)
		var priorHash []byte
		var priorResult []byte
		err = tx.QueryRowContext(ctx, "SELECT payload_hash,result FROM "+s.schema+".case_manager_resource_reviews WHERE tenant_id=? AND operation_id=?", tenant, mysqlstore.UUID(command.OperationID)).Scan(&priorHash, &priorResult)
		if err == nil {
			if string(priorHash) != string(digest[:]) {
				return identity.Conflict()
			}
			return json.Unmarshal(priorResult, &out)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if !before.Eligible || before.Version != command.ExpectedVersion {
			return identity.Conflict()
		}
		if command.Action != "approve" && command.Action != "decline" {
			return identity.Invalid("Review action is unavailable.")
		}
		if reason := before.DisabledActions[command.Action]; reason != "" {
			return identity.Invalid(reason)
		}
		out = state(outcome, command.Action)
		result, err := json.Marshal(out)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO "+s.schema+".case_manager_resource_reviews(tenant_id,operation_id,case_id,resource_id,actor_id,status,findings,payload_hash,result,created_at) VALUES(?,?,?,?,?,?,?,?,?,UTC_TIMESTAMP(6))", tenant, mysqlstore.UUID(command.OperationID), mysqlstore.UUID(caseID), mysqlstore.UUID(decision), mysqlstore.UUID(s.session.ActorID), command.Action, command.Findings, digest[:], mysqlstore.JSON(result)); err != nil {
			return err
		}
		payload, err := json.Marshal(map[string]any{"operation_id": command.OperationID, "resource_id": decision, "actor_id": s.session.ActorID, "owner": "decision_engine", "previous_status": before.Status, "status": out.Status, "findings": command.Findings})
		if err != nil {
			return err
		}
		event := mysqlstore.UUID(uuid.New())
		if _, err = tx.ExecContext(ctx, "INSERT INTO "+s.schema+".case_manager_case_events(id,tenant_id,case_id,event_type,payload,created_at) VALUES(?,?,?,'investigator_review_recorded',?,UTC_TIMESTAMP(6))", event, tenant, mysqlstore.UUID(caseID), mysqlstore.JSON(payload)); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO "+s.schema+".case_manager_outbox_events(id,tenant_id,aggregate_type,aggregate_id,event_type,payload,status,created_at) VALUES(?,?,'case',?,'investigator_review_recorded',?,'pending',UTC_TIMESTAMP(6))", event, tenant, mysqlstore.UUID(caseID), mysqlstore.JSON(payload))
		return err
	})
	var duplicate *driver.MySQLError
	if errors.As(err, &duplicate) && duplicate.Number == 1062 {
		return identity.State{}, identity.Conflict()
	}
	return out, err
}

// CreateCaseForDecision is a local operator bootstrap capability used by the
// acceptance CLI, never a public decision endpoint or an external dispatcher.
func (s *Store) CreateCaseForDecision(ctx context.Context, decision uuid.UUID) (id uuid.UUID, err error) {
	id = uuid.New()
	err = mysqlstore.Mutation(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.authorize(ctx, tx, true); err != nil {
			return err
		}
		tenant := mysqlstore.UUID(s.session.TenantID)
		var inbox mysqlstore.UUID
		if err := tx.QueryRowContext(ctx, "SELECT i.id FROM "+s.schema+".case_manager_inboxes i JOIN "+s.schema+".case_manager_inbox_users u ON u.tenant_id=i.tenant_id AND u.inbox_id=i.id WHERE i.tenant_id=? AND u.user_id=? AND i.active=TRUE ORDER BY i.id LIMIT 1 FOR SHARE", tenant, mysqlstore.UUID(s.session.ActorID)).Scan(&inbox); err != nil {
			return err
		}
		var scenario mysqlstore.UUID
		var object, kind string
		if err := tx.QueryRowContext(ctx, "SELECT scenario_id,object_id,object_type FROM "+s.schema+".decision_engine_decisions WHERE tenant_id=? AND id=?", tenant, mysqlstore.UUID(decision)).Scan(&scenario, &object, &kind); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO "+s.schema+".case_manager_cases(id,tenant_id,inbox_id,name,status,outcome,type,created_at,updated_at) VALUES(?,?,?,'Local decision review','investigating','unset','decision',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))", mysqlstore.UUID(id), tenant, inbox); err != nil {
			return err
		}
		pivot := sha256.Sum256([]byte(kind + ":" + object))
		_, err := tx.ExecContext(ctx, "INSERT INTO "+s.schema+".case_manager_case_decisions(id,tenant_id,case_id,decision_id,scenario_id,object_type,object_id,pivot_hash,pivot_value,created_at) VALUES(?,?,?,?,?,?,?,?,?,UTC_TIMESTAMP(6))", mysqlstore.UUID(uuid.New()), tenant, mysqlstore.UUID(id), mysqlstore.UUID(decision), scenario, []byte(kind), []byte(object), pivot[:], []byte(object))
		return err
	})
	return id, err
}
