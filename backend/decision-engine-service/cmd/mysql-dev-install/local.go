package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/localtenants"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqlrelease"
	R "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqlruntime"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"github.com/google/uuid"
)

func configureLocal(ctx context.Context, db *sql.DB, plan migrations.Plan) error {
	if err := installReviewOwners(ctx); err != nil {
		return err
	}
	deliveryPlan, err := installDeliveries(ctx)
	if err != nil {
		return err
	}
	tenant, err := uuid.Parse(os.Getenv("DECISION_TENANT_ID"))
	if err != nil || tenant == uuid.Nil {
		return errors.New("prepared local tenant required")
	}
	legacyReviewTenant := tenant
	if raw, err := os.ReadFile("/runtime/config.json"); err == nil {
		var previous R.LocalConfig
		if err := json.Unmarshal(raw, &previous); err != nil {
			return err
		}
		if previous.ReviewLegacyTenantID != uuid.Nil {
			legacyReviewTenant = previous.ReviewLegacyTenantID
		} else if previous.TenantID != uuid.Nil {
			legacyReviewTenant = previous.TenantID
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	ip, subnet, err := net.ParseCIDR(os.Getenv("DECISION_MYSQL_SUBNET"))
	if err != nil || ip.To4() == nil {
		return errors.New("explicit local Docker IPv4 subnet required")
	}
	host := ip.Mask(subnet.Mask).String() + "/" + net.IP(subnet.Mask).String()
	if err := configureProviderAccount(ctx, db, host); err != nil {
		return err
	}
	var active bool
	if err := db.QueryRowContext(ctx, "SELECT status='active' FROM fraud.data_model_tenants WHERE id=?", mysqlstore.UUID(tenant)).Scan(&active); err != nil {
		return err
	}
	if !active {
		return errors.New("local tenant must be active")
	}
	registry, err := localtenants.New(db, "fraud")
	if err != nil {
		return err
	}
	var after uuid.UUID
	for {
		page, err := registry.Scan(ctx, after, 100)
		if err != nil {
			return err
		}
		for _, entry := range page {
			after = entry.ID
			if entry.Active {
				if err := seedReviewIdentity(ctx, db, entry.ID, legacyReviewTenant); err != nil {
					return err
				}
			}
		}
		if len(page) < 100 {
			break
		}
	}
	if err := localEffectFixture(ctx, db, tenant); err != nil {
		return err
	}
	for _, worker := range []bool{false, true} {
		user, secret, role := R.LocalAPIUser, "decision_api_password", "api"
		if worker {
			user, secret, role = R.LocalWorkerUser, "decision_worker_password", "worker"
		}
		password, err := R.LocalSecret(secret)
		if err != nil {
			return err
		}
		account := "`" + user + "`@'" + host + "'"
		statements := []string{"CREATE USER IF NOT EXISTS " + account + " IDENTIFIED BY '" + password + "'", "ALTER USER " + account + " IDENTIFIED BY '" + password + "'", "REVOKE ALL PRIVILEGES, GRANT OPTION FROM " + account}
		grants, err := mysqlrelease.OwnerGrants("fraud", user, host, role)
		if err != nil {
			return err
		}
		statements = append(statements, grants...)
		statements = append(statements, "GRANT SELECT ON fraud.decision_local_schema_migrations TO "+account, "GRANT SELECT ON fraud.decision_local_delivery_receipts TO "+account)
		if !worker {
			statements = append(statements, reviewGrants(account)...)
		}
		for _, table := range []string{"data_model_tenants", "data_model_tables", "data_model_fields", "data_model_physical_tables", "data_model_physical_fields", "data_model_ingestion_contracts", "ingestion_velocity_definitions", "ingestion_velocity_buckets"} {
			statements = append(statements, "GRANT SELECT ON fraud.`"+table+"` TO "+account)
		}
		// Registry FOR SHARE is read-only; no data-model authoring grants are given.
		statements = append(statements, "GRANT EXECUTE ON FUNCTION fraud.data_model_find_record_object_key_v1 TO "+account)
		for _, statement := range statements {
			if _, err := db.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("configure local decision %s: %w", role, err)
			}
		}
		if err := grantLocalPublishedReads(ctx, db, account); err != nil {
			return err
		}
	}
	if _, err := R.LocalSecret("decision_runtime_token"); err != nil {
		return err
	}
	data, err := json.MarshalIndent(R.LocalConfig{TenantMode: "all-active", ReviewLegacyTenantID: legacyReviewTenant, TenantID: tenant, ReviewerID: uuid.NewSHA1(tenant, []byte("local-decision-reviewer")), ReviewInboxID: uuid.NewSHA1(tenant, []byte("local-decision-review-inbox")), AccountHost: host, Plan: plan, DeliveryPlan: deliveryPlan}, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("/runtime", ".config-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), "/runtime/config.json"); err != nil {
		return err
	}
	fmt.Printf("local decision scope: all active registered tenants; default fixture tenant=%s; explicit account network=%s\n", tenant, host)
	return nil
}

// Grant only physical published record tables, across every tenant. Pages and
// in-memory grant buffers are bounded; no database-wide SELECT grant is used.
func grantLocalPublishedReads(ctx context.Context, db *sql.DB, account string) error {
	var tenant, table uuid.UUID
	for {
		rows, err := db.QueryContext(ctx, "SELECT p.tenant_id,p.table_id,p.physical_name FROM fraud.data_model_physical_tables p JOIN fraud.data_model_tables t ON t.tenant_id=p.tenant_id AND t.id=p.table_id WHERE (p.tenant_id>? OR(p.tenant_id=? AND p.table_id>?)) AND p.write_state='open' AND t.archived=FALSE ORDER BY p.tenant_id,p.table_id LIMIT 100", mysqlstore.UUID(tenant), mysqlstore.UUID(tenant), mysqlstore.UUID(table))
		if err != nil {
			return err
		}
		names := make([]string, 0, 100)
		for rows.Next() {
			var t, b mysqlstore.UUID
			var name string
			if err := rows.Scan(&t, &b, &name); err != nil {
				rows.Close()
				return err
			}
			tenant, table = uuid.UUID(t), uuid.UUID(b)
			quoted, err := mysqlstore.Identifier(name)
			if err != nil {
				rows.Close()
				return err
			}
			names = append(names, quoted)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, name := range names {
			if _, err := db.ExecContext(ctx, "GRANT SELECT ON fraud."+name+" TO "+account); err != nil {
				return err
			}
		}
		if len(names) < 100 {
			return nil
		}
	}
}
