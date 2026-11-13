package ec2test

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

const KeycloakUser = "fraud_ec2_keycloak"

// ProvisionKeycloak is an explicit operator step, never a runtime privilege.
// It creates only the provider-owned database/account. Existing databases and
// passwords are never replaced; an interrupted DDL sequence can be resumed.
func ProvisionKeycloak(ctx context.Context, admin *sql.DB, profile Config, password string) error {
	parts := strings.Split(profile.AccountHost, "/")
	if len(parts) != 2 || net.ParseIP(parts[0]).To4() == nil || parts[1] != "255.255.255.255" || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(password) {
		return errors.New("explicit Keycloak owner credential and single source address required")
	}
	account := "'" + KeycloakUser + "'@'" + profile.AccountHost + "'"
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS keycloak CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		return errors.New("cannot provision the Keycloak database")
	}
	var collation string
	if err := admin.QueryRowContext(ctx, "SELECT DEFAULT_COLLATION_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME='keycloak'").Scan(&collation); err != nil || collation != "utf8mb4_unicode_ci" {
		return errors.New("retained Keycloak database must use utf8mb4_unicode_ci; no database was altered")
	}
	if _, err := admin.ExecContext(ctx, "CREATE USER IF NOT EXISTS "+account+" IDENTIFIED BY '"+password+"' REQUIRE SSL"); err != nil {
		return errors.New("cannot provision the restricted Keycloak account")
	}
	// Fail on an unexpectedly privileged retained account instead of revoking
	// somebody else's grants or letting the identity provider read fraud tables.
	for _, table := range []string{"USER_PRIVILEGES", "SCHEMA_PRIVILEGES", "TABLE_PRIVILEGES", "COLUMN_PRIVILEGES"} {
		condition := "(TABLE_SCHEMA<>'keycloak' OR IS_GRANTABLE='YES')"
		if table == "USER_PRIVILEGES" {
			condition = "(PRIVILEGE_TYPE<>'USAGE' OR IS_GRANTABLE='YES')"
		}
		var count int
		if err := admin.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema."+table+" WHERE GRANTEE=? AND "+condition, account).Scan(&count); err != nil || count != 0 {
			return errors.New("Keycloak account has unexpected grants or account metadata is unavailable")
		}
	}
	var other int
	if err := admin.QueryRowContext(ctx, "SELECT COUNT(*) FROM mysql.procs_priv WHERE User=? AND Host=? AND (Db<>'keycloak' OR FIND_IN_SET('Grant',Proc_priv)>0)", KeycloakUser, profile.AccountHost).Scan(&other); err != nil || other != 0 {
		return errors.New("Keycloak account has unexpected routine grants or account metadata is unavailable")
	}
	if err := admin.QueryRowContext(ctx, "SELECT COUNT(*) FROM mysql.global_grants WHERE USER=? AND HOST=?", KeycloakUser, profile.AccountHost).Scan(&other); err != nil || other != 0 {
		return errors.New("Keycloak account must have no global dynamic grants")
	}
	var roles int
	if err := admin.QueryRowContext(ctx, "SELECT COUNT(*) FROM mysql.role_edges WHERE TO_USER=? AND TO_HOST=?", KeycloakUser, profile.AccountHost).Scan(&roles); err != nil || roles != 0 {
		return errors.New("Keycloak account roles must be absent and verifiable")
	}
	for _, query := range []string{"ALTER USER " + account + " REQUIRE SSL", "GRANT ALL PRIVILEGES ON keycloak.* TO " + account} {
		if _, err := admin.ExecContext(ctx, query); err != nil {
			return errors.New("cannot apply the Keycloak database-only grants")
		}
	}
	tls, err := profile.tls()
	if err != nil {
		return err
	}
	owner, err := mysqlstore.Open(ctx, mysqlstore.Config{Address: profile.Address, Database: "keycloak", User: KeycloakUser, Password: password, TLS: tls, MaxOpen: 1, MaxIdle: 1, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if err != nil {
		return errors.New("retained Keycloak owner password or verified TLS login failed; no password was reset")
	}
	defer owner.Close()
	if err := owner.PingContext(ctx); err != nil {
		return errors.New("retained Keycloak owner password or verified TLS login failed; no password was reset")
	}
	return nil
}
