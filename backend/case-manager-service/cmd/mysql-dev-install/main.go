package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	R "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/mysqlruntime"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/mysqlcase"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/localdev"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, "case install failed:", e)
		os.Exit(1)
	}
}
func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	steps, e := mysqlcase.Statements("fraud", "local_case_guard")
	if e != nil {
		return e
	}
	grants, e := mysqlcase.DefinerGrants("fraud", "local_case_guard")
	if e != nil {
		return e
	}
	return localdev.Install(ctx, localdev.Installation{Owner: "case_manager", Steps: steps, Accounts: []string{"local_case_guard"}, Grants: grants, Upgrade: extendLocalOwnerPlan(steps), Verify: configure})
}
func configure(ctx context.Context, db *sql.DB, p migrations.Plan) error {
	c, e := R.Load("/runtime/config.json")
	if e != nil {
		return e
	}
	var noncanonical bool
	if e = db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM fraud.case_manager_cases WHERE assigned_to IS NOT NULL AND IF(IS_UUID(assigned_to),assigned_to<>BIN_TO_UUID(UUID_TO_BIN(assigned_to)),TRUE))").Scan(&noncanonical); e != nil {
		return e
	}
	if noncanonical {
		return errors.New("existing case assignees require explicit reconciliation to canonical identity UUIDs; no rows were changed")
	}
	parts := strings.Split(c.AccountHost, "/")
	if len(parts) != 2 || net.ParseIP(parts[0]).To4() == nil || net.ParseIP(parts[1]).To4() == nil {
		return errors.New("explicit Docker IPv4 network required")
	}
	for _, role := range []string{"api", "worker"} {
		password, e := R.Secret("case_" + role + "_password")
		if e != nil {
			return e
		}
		account := "`local_case_" + role + "`@'" + c.AccountHost + "'"
		for _, q := range []string{"CREATE USER IF NOT EXISTS " + account + " IDENTIFIED BY '" + password + "'", "ALTER USER " + account + " IDENTIFIED BY '" + password + "'", "REVOKE ALL PRIVILEGES, GRANT OPTION FROM " + account} {
			if _, e = db.ExecContext(ctx, q); e != nil {
				return errors.New("case account configuration failed")
			}
		}
		grant := func(privilege, table string) error {
			_, e := db.ExecContext(ctx, "GRANT "+privilege+" ON fraud."+table+" TO "+account)
			return e
		}
		for _, table := range []string{"case_manager_cases", "case_manager_inboxes", "case_manager_inbox_users", "case_manager_tags", "case_manager_case_tags", "case_manager_case_decisions", "case_manager_case_screenings", "case_manager_case_events", "case_manager_case_files", "case_manager_contributors", "case_manager_intake_receipts", "case_manager_workflow_locks", "case_manager_continuous_bindings", "case_manager_reports", "case_manager_evidence_uploads", "case_manager_outbox_events", "case_manager_inbox_events", "case_manager_inbox_outbox", "identity_tenants", "identity_users", "identity_memberships"} {
			if e = grant("SELECT", table); e != nil {
				return e
			}
		}
		if role == "api" {
			for _, table := range []string{"case_manager_cases", "case_manager_inboxes", "case_manager_inbox_users", "case_manager_tags", "case_manager_case_tags", "case_manager_case_decisions", "case_manager_case_screenings", "case_manager_case_files", "case_manager_contributors", "case_manager_intake_receipts", "case_manager_workflow_locks", "case_manager_continuous_bindings", "case_manager_reports", "case_manager_evidence_uploads"} {
				if e = grant("INSERT,UPDATE", table); e != nil {
					return e
				}
			}
			for _, table := range []string{"case_manager_case_events", "case_manager_outbox_events", "case_manager_inbox_events", "case_manager_inbox_outbox"} {
				if e = grant("INSERT", table); e != nil {
					return e
				}
			}
			if e = grant("DELETE", "case_manager_inbox_users"); e != nil {
				return e
			}
			for _, table := range []string{"decision_engine_decisions", "decision_engine_scenarios", "screening_screenings", "screening_matches", "data_model_tenants", "data_model_tables", "data_model_fields", "data_model_links", "data_model_pivots", "data_model_pivot_path_links", "data_model_navigation_options", "data_model_physical_tables", "data_model_physical_fields"} {
				if e = grant("SELECT", table); e != nil {
					return e
				}
			}
			for _, fn := range []string{"identity_session_roles_v1", "data_model_find_record_object_key_v1"} {
				if _, e = db.ExecContext(ctx, "GRANT EXECUTE ON FUNCTION fraud."+fn+" TO "+account); e != nil {
					return e
				}
			}
			// The screening lifecycle extension can be installed after case setup.
			// Grant only contracts that are present; absent owners still return 503.
			for _, table := range []string{"decision_engine_screening_configs", "screening_continuous_runs", "screening_continuous_configs", "screening_case_event_outbox", "screening_files", "screening_local_blobs"} {
				var present bool
				if e = db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema='fraud' AND table_name=?)", table).Scan(&present); e != nil {
					return e
				}
				if present {
					if e = grant("SELECT", table); e != nil {
						return e
					}
				}
			}
			// Only currently registered, published physical tables; never fraud.*.
			rows, e := db.QueryContext(ctx, "SELECT physical_name FROM fraud.data_model_physical_tables WHERE write_state='open' AND published_version IS NOT NULL ORDER BY physical_name LIMIT 1001")
			if e != nil {
				return e
			}
			var tables []string
			for rows.Next() {
				var name string
				if e = rows.Scan(&name); e != nil {
					rows.Close()
					return e
				}
				if len(tables) == 1000 {
					rows.Close()
					return errors.New("too many physical tables for local case setup")
				}
				quoted, e := mysqlstore.Identifier(name)
				if e != nil {
					rows.Close()
					return e
				}
				tables = append(tables, quoted)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
			for _, table := range tables {
				if e = grant("SELECT", table); e != nil {
					return e
				}
			}
		} else {
			for _, g := range []struct{ priv, table string }{{"UPDATE(assigned_to,snoozed_until,updated_at)", "case_manager_cases"}, {"INSERT", "case_manager_case_events"}, {"INSERT,UPDATE", "case_manager_outbox_events"}, {"UPDATE", "case_manager_inbox_outbox"}, {"DELETE", "case_manager_evidence_uploads"}, {"UPDATE(user_id)", "case_manager_inbox_users"}} {
				if e = grant(g.priv, g.table); e != nil {
					return e
				}
			}
		}
	}
	fmt.Println("case MySQL owner migrations and restricted local accounts verified")
	return nil
}
