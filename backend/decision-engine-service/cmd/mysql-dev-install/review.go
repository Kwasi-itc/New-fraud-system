package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/mysqlidentity"
	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/sessioncredential"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/mysqlcase"
	R "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqlruntime"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/localdev"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/mysqlschema"
	screeningLifecycle "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/mysqlscreening"
	"github.com/google/uuid"
	"os"
)

// Prerequisites retain their own migration histories and locked definers. They
// supply SQL identity/case/source contracts; no delivery worker is activated.
func installReviewOwners(ctx context.Context) error {
	for _, owner := range []struct {
		name, definer string
		steps         func(string, string) ([]migrations.Step, error)
		grants        func(string, string) ([]string, error)
	}{
		{"identity", "local_identity_guard", mysqlidentity.Statements, mysqlidentity.DefinerGrants},
		{"screening", "local_screening_guard", mysqlschema.Statements, mysqlschema.DefinerGrants},
		{"case_manager", "local_case_guard", mysqlcase.Statements, mysqlcase.DefinerGrants},
	} {
		if owner.name == "screening" {
			// A later screening lifecycle deployment retains the publication
			// prefix. Never attempt to downgrade its artifact on decision setup.
			b, e := os.ReadFile("/plans/screening.json")
			if e == nil {
				var p migrations.Plan
				if e = json.Unmarshal(b, &p); e != nil {
					return e
				}
				if e = p.Validate(); e != nil {
					return e
				}
				base, e := mysqlschema.Statements("fraud", owner.definer)
				if e != nil {
					return e
				}
				if len(p.Steps) > len(base) {
					full, e := screeningLifecycle.Statements("fraud", owner.definer)
					if e != nil {
						return e
					}
					if p.Owner != "screening" || p.Database != "fraud" || len(p.Steps) > len(full) {
						return errors.New("unexpected screening lifecycle artifact")
					}
					for i, s := range p.Steps {
						if s.ID != full[i].ID || s.SQL != full[i].SQL || s.Kind != full[i].Kind || s.Name != full[i].Name {
							return migrations.ErrLineage
						}
					}
					continue
				}
			} else if !os.IsNotExist(e) {
				return e
			}
		}
		steps, err := owner.steps("fraud", owner.definer)
		if err != nil {
			return err
		}
		grants, err := owner.grants("fraud", owner.definer)
		if err != nil {
			return err
		}
		if err := localdev.Install(ctx, localdev.Installation{Owner: owner.name, Steps: steps, Accounts: []string{owner.definer}, Grants: grants, Upgrade: extendLocalOwnerPlan(steps)}); err != nil {
			return err
		}
	}
	// The lifecycle needs case inboxes; the case owner itself needs the earlier
	// screening publication prefix. Keep that bootstrap order explicit.
	steps, e := screeningLifecycle.Statements("fraud", "local_screening_guard")
	if e != nil {
		return e
	}
	grants, e := screeningLifecycle.DefinerGrants("fraud", "local_screening_guard")
	if e != nil {
		return e
	}
	if e = localdev.Install(ctx, localdev.Installation{Owner: "screening", Steps: steps, Accounts: []string{"local_screening_guard"}, Grants: grants, Upgrade: extendLocalOwnerPlan(steps)}); e != nil {
		return e
	}
	return nil
}

func seedReviewIdentity(ctx context.Context, db *sql.DB, tenant, fixtureTenant uuid.UUID) error {
	credential, err := R.LocalTenantReviewCredential(tenant, fixtureTenant)
	if err != nil {
		return err
	}
	actor := uuid.NewSHA1(tenant, []byte("local-decision-reviewer"))
	sid := uuid.NewSHA1(tenant, []byte("local-decision-review-session"))
	inbox := uuid.NewSHA1(tenant, []byte("local-decision-review-inbox"))
	return mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
		// Repeated setup preserves revocation, membership versions and activation.
		for _, item := range []struct {
			sql  string
			args []any
		}{
			{"INSERT INTO fraud.identity_tenants(id,active) VALUES(?,TRUE) ON DUPLICATE KEY UPDATE id=id", []any{mysqlstore.UUID(tenant)}},
			{"INSERT INTO fraud.identity_users(id,display_name,active) VALUES(?,'Local development decision reviewer',TRUE) ON DUPLICATE KEY UPDATE id=id", []any{mysqlstore.UUID(actor)}},
			{"INSERT INTO fraud.identity_memberships(tenant_id,user_id,roles,active,version) SELECT ?,?,'case_manager_admin,platform_admin',TRUE,1 WHERE NOT EXISTS(SELECT 1 FROM fraud.identity_memberships WHERE tenant_id=? AND user_id=?)", []any{mysqlstore.UUID(tenant), mysqlstore.UUID(actor), mysqlstore.UUID(tenant), mysqlstore.UUID(actor)}},
			{"INSERT INTO fraud.identity_sessions(id,digest,tenant_id,user_id,created_at,expires_at,idle_until) VALUES(?,?,?,?,UTC_TIMESTAMP(6),DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 7 DAY),DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 7 DAY)) ON DUPLICATE KEY UPDATE expires_at=VALUES(expires_at),idle_until=VALUES(idle_until)", []any{mysqlstore.UUID(sid), sessioncredential.Digest(credential), mysqlstore.UUID(tenant), mysqlstore.UUID(actor)}},
			{"INSERT INTO fraud.case_manager_inboxes(id,tenant_id,name,active) VALUES(?,?,'Local decision review',TRUE) ON DUPLICATE KEY UPDATE id=id", []any{mysqlstore.UUID(inbox), mysqlstore.UUID(tenant)}},
			{"INSERT INTO fraud.case_manager_inbox_users(tenant_id,inbox_id,user_id) VALUES(?,?,?) ON DUPLICATE KEY UPDATE user_id=user_id", []any{mysqlstore.UUID(tenant), mysqlstore.UUID(inbox), mysqlstore.UUID(actor)}},
		} {
			if _, err := tx.ExecContext(ctx, item.sql, item.args...); err != nil {
				return err
			}
		}
		return nil
	})
}

func reviewGrants(account string) []string {
	var out []string
	out = append(out, "GRANT SELECT,INSERT,UPDATE ON fraud.case_manager_workflow_locks TO "+account)
	// FOR UPDATE requires a locking privilege. The source ID is immutable,
	// and its trigger also requires the next publication version; this column
	// grant cannot modify an owner result or acknowledge its projection queue.
	out = append(out, "GRANT UPDATE(id) ON fraud.screening_screenings TO "+account)
	out = append(out, "GRANT UPDATE(id) ON fraud.screening_decision_source_v1 TO "+account)
	for _, table := range []string{"identity_tenants", "identity_users", "identity_memberships", "identity_sessions", "case_manager_cases", "case_manager_inboxes", "case_manager_inbox_users", "case_manager_case_decisions", "case_manager_resource_reviews", "screening_decision_source_v1", "screening_screenings"} {
		out = append(out, "GRANT SELECT ON fraud."+table+" TO "+account)
	}
	for _, name := range []string{"identity_session_roles_v1", "identity_authorize_decision_v1", "case_manager_decision_inboxes_v1", "case_manager_authorize_decision_inboxes_v1", "case_manager_authorize_decision_inbox_v1", "case_manager_authorize_decision_tags_v1", "case_manager_visible_decision_inboxes_v1"} {
		out = append(out, "GRANT EXECUTE ON FUNCTION fraud."+name+" TO "+account)
	}
	for _, table := range []string{"case_manager_cases", "case_manager_case_decisions", "case_manager_resource_reviews", "case_manager_case_events", "case_manager_outbox_events"} {
		out = append(out, "GRANT INSERT ON fraud."+table+" TO "+account)
	}
	return out
}
