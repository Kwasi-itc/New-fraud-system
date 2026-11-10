package mysqlruntime

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
	"regexp"
)

type localConsumerAccount struct{ User, Host string }

var localConsumerHost = regexp.MustCompile(`^[A-Za-z0-9_.:%/-]{1,255}$`)

func localConsumerGrant(physical, user, host string) (string, error) {
	table, err := mysqlstore.Identifier(physical)
	if err != nil {
		return "", err
	}
	privileges := "SELECT"
	switch user {
	case "local_ingestion_api", "local_ingestion_worker":
		privileges = "SELECT,INSERT,UPDATE"
	case "local_dm_api", "local_decision_api", "local_decision_worker", "local_case_api", "local_case_worker":
	default:
		return "", errors.New("unknown local runtime consumer")
	}
	if !localConsumerHost.MatchString(host) {
		return "", errors.New("invalid existing local account host")
	}
	account, err := mysqlstore.Identifier(user)
	if err != nil {
		return "", err
	}
	// MySQL runtime accounts use netmask hosts; locked owner definers intentionally
	// have a narrower host policy. This renderer cannot create/unlock accounts or
	// issue schema-wide grants, DDL privileges, DELETE, or GRANT OPTION.
	return "GRANT " + privileges + " ON `fraud`." + table + " TO " + account + "@'" + host + "'", nil
}

// Local development consumers receive grants only on an attested physical table.
// Metadata, credentials, schema-wide privileges and GRANT OPTION are excluded.
// Existing service accounts are used; the data owner never creates/unlocks them.
func (p *LocalCreation) grantLocalConsumers(ctx context.Context, physical string) error {
	if p == nil || p.Target == nil {
		return errors.New("local physical owner required")
	}
	if _, err := mysqlstore.Identifier(physical); err != nil {
		return err
	}
	rows, err := p.Target.QueryContext(ctx, "SELECT User,Host FROM mysql.user WHERE User IN('local_dm_api','local_ingestion_api','local_ingestion_worker','local_decision_api','local_decision_worker','local_case_api','local_case_worker') ORDER BY User,Host LIMIT 33")
	if err != nil {
		return err
	}
	accounts := []localConsumerAccount{}
	for rows.Next() {
		var a localConsumerAccount
		if err := rows.Scan(&a.User, &a.Host); err != nil {
			rows.Close()
			return err
		}
		accounts = append(accounts, a)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	if len(accounts) > 32 {
		return errors.New("too many local runtime account bindings")
	}
	for _, account := range accounts {
		statement, err := localConsumerGrant(physical, account.User, account.Host)
		if err != nil {
			return err
		}
		if _, err := p.Target.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

// SyncLocalConsumers repairs only local consumer grants on already published
// tables. Keyset pages also advance over inactive/closed metadata; no unbounded
// collection, data mutation, DDL, or business-work retry is performed.
func (p *LocalCreation) SyncLocalConsumers(ctx context.Context) error {
	var tenant, table uuid.UUID
	for {
		rows, err := p.Target.QueryContext(ctx, "SELECT p.tenant_id,p.table_id,p.physical_name,(p.write_state='open' AND p.published_version IS NOT NULL AND t.archived=FALSE AND n.status='active') FROM fraud.data_model_physical_tables p JOIN fraud.data_model_tables t ON t.tenant_id=p.tenant_id AND t.id=p.table_id JOIN fraud.data_model_tenants n ON n.id=p.tenant_id WHERE (p.tenant_id>? OR (p.tenant_id=? AND p.table_id>?)) ORDER BY p.tenant_id,p.table_id LIMIT 100", mysqlstore.UUID(tenant), mysqlstore.UUID(tenant), mysqlstore.UUID(table))
		if err != nil {
			return err
		}
		names := []string{}
		count := 0
		for rows.Next() {
			var a, b mysqlstore.UUID
			var name string
			var active bool
			if err := rows.Scan(&a, &b, &name, &active); err != nil {
				rows.Close()
				return err
			}
			tenant, table = uuid.UUID(a), uuid.UUID(b)
			count++
			if active {
				names = append(names, name)
			}
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		for _, name := range names {
			if err := p.grantLocalConsumers(ctx, name); err != nil {
				return err
			}
		}
		if count < 100 {
			return nil
		}
	}
}
