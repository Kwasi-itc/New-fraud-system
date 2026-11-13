package ec2test

import (
	"context"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

func TestKeycloakRejectsUnboundedAccountsBeforeSQL(t *testing.T) {
	for _, host := range []string{"%", "10.0.0.0/255.255.0.0", "not-an-ip/255.255.255.255"} {
		if err := ProvisionKeycloak(context.Background(), nil, Config{AccountHost: host}, strings.Repeat("c", 64)); err == nil {
			t.Fatal(host)
		}
	}
}

func TestKeycloakMySQLProvisionPreservesCredentialsAndIsolation(t *testing.T) {
	address := os.Getenv("EC2_KEYCLOAK_TEST_ADDRESS")
	if address == "" {
		t.Skip("private EC2 MySQL fixture not configured")
	}
	if address != "mysql:3306" {
		t.Fatal("isolated Compose fixture required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	connection, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	source := connection.LocalAddr().(*net.TCPAddr).IP.String()
	connection.Close()
	profile := Config{Address: address, CAFile: os.Getenv("EC2_KEYCLOAK_TEST_CA"), AccountHost: source + "/255.255.255.255"}
	tls, err := profile.tls()
	if err != nil {
		t.Fatal(err)
	}
	admin, err := mysqlstore.Open(ctx, mysqlstore.Config{Address: address, Database: "information_schema", User: "root", Password: "isolated-ec2-test-root", TLS: tls, MaxOpen: 1, MaxIdle: 1, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	password := strings.Repeat("c", 64)
	for attempt := 0; attempt < 2; attempt++ {
		if err := ProvisionKeycloak(ctx, admin, profile, password); err != nil {
			t.Fatal(err)
		}
	}
	if err := ProvisionKeycloak(ctx, admin, profile, strings.Repeat("d", 64)); err == nil {
		t.Fatal("retained account password must not be reset")
	}
	if err := ProvisionKeycloak(ctx, admin, profile, password); err != nil {
		t.Fatal("original credential must remain valid", err)
	}
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS fraud"); err != nil {
		t.Fatal(err)
	}
	account := "'" + KeycloakUser + "'@'" + profile.AccountHost + "'"
	if _, err := admin.ExecContext(ctx, "CREATE ROLE IF NOT EXISTS ec2_keycloak_fixture_role"); err != nil {
		t.Fatal(err)
	}
	defer admin.ExecContext(ctx, "DROP ROLE ec2_keycloak_fixture_role")
	for _, privilege := range []struct{ grant, revoke string }{
		{"GRANT SELECT ON fraud.* TO ", "REVOKE SELECT ON fraud.* FROM "},
		{"GRANT PROCESS ON *.* TO ", "REVOKE PROCESS ON *.* FROM "},
		{"GRANT CONNECTION_ADMIN ON *.* TO ", "REVOKE CONNECTION_ADMIN ON *.* FROM "},
		{"GRANT SELECT ON keycloak.* TO ", "REVOKE GRANT OPTION ON keycloak.* FROM "},
		{"GRANT ec2_keycloak_fixture_role TO ", "REVOKE ec2_keycloak_fixture_role FROM "},
	} {
		grant := privilege.grant + account
		if strings.Contains(privilege.revoke, "GRANT OPTION") {
			grant += " WITH GRANT OPTION"
		}
		if _, err := admin.ExecContext(ctx, grant); err != nil {
			t.Fatal(err)
		}
		if err := ProvisionKeycloak(ctx, admin, profile, password); err == nil {
			t.Fatal("unexpected privilege must be rejected", privilege.grant)
		}
		if _, err := admin.ExecContext(ctx, privilege.revoke+account); err != nil {
			t.Fatal(err)
		}
	}
	var sslType string
	if err := admin.QueryRowContext(ctx, "SELECT ssl_type FROM mysql.user WHERE User=? AND Host=?", KeycloakUser, profile.AccountHost).Scan(&sslType); err != nil || sslType != "ANY" {
		t.Fatal("provider account must require SSL", sslType, err)
	}
	if err := ProvisionKeycloak(ctx, admin, profile, password); err != nil {
		t.Fatal(err)
	}
	owner, err := mysqlstore.Open(ctx, mysqlstore.Config{Address: address, Database: "keycloak", User: KeycloakUser, Password: password, TLS: tls, MaxOpen: 1, MaxIdle: 1, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if _, err := owner.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS keycloak.owner_retention_fixture(id INT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.ExecContext(ctx, "INSERT IGNORE INTO keycloak.owner_retention_fixture VALUES(7)"); err != nil {
		t.Fatal(err)
	}
	if err := ProvisionKeycloak(ctx, admin, profile, password); err != nil {
		t.Fatal(err)
	}
	var retained int
	if err := owner.QueryRowContext(ctx, "SELECT id FROM keycloak.owner_retention_fixture").Scan(&retained); err != nil || retained != 7 {
		t.Fatal("provider data changed", retained, err)
	}
	if _, err := owner.ExecContext(ctx, "CREATE TABLE fraud.owner_escape_fixture(id INT)"); err == nil {
		t.Fatal("provider account escaped its database")
	}
	if _, err := owner.ExecContext(ctx, "DROP TABLE keycloak.owner_retention_fixture"); err != nil {
		t.Fatal(err)
	}
}
