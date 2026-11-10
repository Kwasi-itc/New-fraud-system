package mysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/mysqlcase"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
	"strings"
)

type ReviewApply func(context.Context, *Store, *identity.Command) (identity.State, error)

func (s *Store) caseAuthority(ctx context.Context, p identity.Principal, tenant, caseID uuid.UUID, resource string, lock bool) (string, bool, error) {
	if p.TenantID != tenant || tenant == uuid.Nil || caseID == uuid.Nil {
		return "", false, identity.Forbidden()
	}
	var inbox string
	var status string
	var active, member bool
	q := "SELECT c.inbox_id,c.status,i.active,EXISTS(SELECT 1 FROM " + s.schema + ".case_manager_inbox_users u WHERE u.tenant_id=c.tenant_id AND u.inbox_id=c.inbox_id AND u.user_id=?) FROM " + s.schema + ".case_manager_cases c JOIN " + s.schema + ".case_manager_inboxes i ON i.tenant_id=c.tenant_id AND i.id=c.inbox_id WHERE c.tenant_id=? AND c.id=?"
	if lock {
		q += " FOR UPDATE OF c FOR SHARE OF i"
	} else {
		q += " FOR SHARE OF c,i"
	}
	e := s.q.QueryRowContext(ctx, q, id(p.Subject), mysqlstore.UUID(tenant), mysqlstore.UUID(caseID)).Scan(uid(&inbox), &status, &active, &member)
	if errors.Is(e, sql.ErrNoRows) {
		return "", false, identity.Forbidden()
	}
	if e != nil {
		return "", false, normalize(e)
	}
	if !p.Admin && !member {
		return "", false, identity.Forbidden()
	}
	if resource != "" {
		var linked bool
		e = s.q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+s.schema+".case_manager_case_screenings WHERE tenant_id=? AND case_id=? AND match_id=?)", mysqlstore.UUID(tenant), mysqlstore.UUID(caseID), id(resource)).Scan(&linked)
		if e != nil {
			return "", false, normalize(e)
		}
		if !linked {
			return "", false, identity.Forbidden()
		}
	}
	return status, active, nil
}
func (s *Store) Review(ctx context.Context, p identity.Principal, tenant, caseID uuid.UUID, resource string, cmd *identity.Command, apply ReviewApply) (out identity.State, err error) {
	if _, e := uuid.Parse(resource); e != nil {
		return out, identity.Invalid("Invalid screening match.")
	}
	if cmd != nil {
		cmd.Findings = strings.TrimSpace(cmd.Findings)
		if cmd.ResourceID != resource || cmd.OperationID == uuid.Nil || cmd.ExpectedVersion == "" || len(cmd.Findings) == 0 || len(cmd.Findings) > 9900 {
			return out, identity.Invalid("Operation, resource, version and findings (1-9900 bytes) required.")
		}
	}
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	r := s.InTransaction(tx)
	if e = r.CheckIdentity(ctx, p, true); e != nil {
		return out, e
	}
	if cmd != nil {
		// The operation mutex is screening-owned, persistent and specific to one
		// command. It does not serialize unrelated case work or depend on a lease.
		_, e = r.q.ExecContext(ctx, "INSERT INTO "+r.table("review_keys")+"(tenant_id,operation_id) VALUES(?,?) ON DUPLICATE KEY UPDATE operation_id=operation_id", mysqlstore.UUID(tenant), mysqlstore.UUID(cmd.OperationID))
		if e != nil {
			return out, normalize(e)
		}
	}
	status, active, e := r.caseAuthority(ctx, p, tenant, caseID, resource, true)
	if e != nil {
		return out, e
	}
	var digest [32]byte
	if cmd != nil {
		raw, _ := json.Marshal(struct {
			Command *identity.Command
			Actor   string
			Case    uuid.UUID
		}{cmd, p.Subject, caseID})
		digest = sha256.Sum256(raw)
		var oldHash, oldResult []byte
		e = r.q.QueryRowContext(ctx, "SELECT payload_hash,result FROM "+r.table("reviews")+" WHERE tenant_id=? AND operation_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(cmd.OperationID)).Scan(&oldHash, &oldResult)
		if e == nil {
			if string(oldHash) != string(digest[:]) {
				return out, identity.Conflict()
			}
			if e = json.Unmarshal(oldResult, &out); e != nil {
				return out, e
			}
			return out, tx.Commit()
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return out, normalize(e)
		}
		if status == "closed" || !active {
			return out, identity.Invalid("Reopen the case in an active inbox before reviewing.")
		}
	}
	before, e := apply(ctx, r, nil)
	if e != nil {
		return out, e
	}
	if cmd == nil {
		before.Eligible = before.Eligible && status != "closed" && active
		return before, tx.Commit()
	}
	if !before.Eligible || before.Version != cmd.ExpectedVersion {
		return out, identity.Conflict()
	}
	allowed := false
	for _, action := range before.Actions {
		allowed = allowed || action == cmd.Action
	}
	if !allowed {
		return out, identity.Invalid("Action is unavailable.")
	}
	out, e = apply(ctx, r, cmd)
	if e != nil {
		return out, e
	}
	out.Eligible = false
	result, e := json.Marshal(out)
	if e != nil {
		return out, e
	}
	_, e = r.q.ExecContext(ctx, "INSERT INTO "+r.table("reviews")+"(tenant_id,operation_id,case_id,resource_id,actor_id,payload_hash,result,previous_status,status,findings,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,UTC_TIMESTAMP(6))", mysqlstore.UUID(tenant), mysqlstore.UUID(cmd.OperationID), mysqlstore.UUID(caseID), id(resource), id(p.Subject), digest[:], mysqlstore.JSON(result), before.Status, out.Status, cmd.Findings)
	if e != nil {
		return out, normalize(e)
	}
	actor, e := uuid.Parse(p.Subject)
	if e != nil {
		return out, identity.Forbidden()
	}
	database := strings.Trim(s.schema, "`")
	if e = mysqlcase.RecordScreeningReview(ctx, tx, database, tenant, cmd.OperationID, actor, p.SessionID, p.AuthzVersion); e != nil {
		return out, e
	}
	return out, tx.Commit()
}
