package main

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/ec2test"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/localdev"
	driver "github.com/go-sql-driver/mysql"
)

// Operator-only upgrade of the browser read contract; no owner DDL, credentials,
// account creation, role changes or data mutations are performed.
func grantLocalBrowser(ctx context.Context) error {
	if _, err := localdev.Origin(os.Getenv("LOCAL_MYSQL_NO_AUTH"), os.Getenv("APP_ORIGIN")); err != nil {
		return err
	}
	raw, err := os.ReadFile("/run/secrets/mysql_root_password")
	if err != nil {
		return err
	}
	password := string(raw)
	if !ec2test.Enabled() {
		password = strings.TrimSpace(password)
	}
	decoded, err := hex.DecodeString(password)
	if !ec2test.Enabled() && (err != nil || len(decoded) != 32) {
		return errors.New("generated local installer secret required")
	}
	cfg := driver.NewConfig()
	cfg.User = "root"
	cfg.Passwd = password
	cfg.Net = "unix"
	cfg.Addr = "/target-socket/mysqld.sock"
	cfg.DBName = "fraud"
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 10 * time.Second
	cfg.WriteTimeout = 10 * time.Second
	if err := ec2test.Driver(cfg, false); err != nil {
		return err
	}
	connector, err := driver.NewConnector(cfg)
	if err != nil {
		return err
	}
	db := sql.OpenDB(connector)
	defer db.Close()
	if _, err := db.ExecContext(ctx, "GRANT EXECUTE ON FUNCTION `fraud`.`case_manager_visible_decision_inboxes_v1` TO `local_case_guard`@'localhost'"); err != nil {
		return err
	}
	rows, err := db.QueryContext(ctx, "SELECT Host FROM mysql.user WHERE User='local_decision_api' ORDER BY Host LIMIT 33")
	if err != nil {
		return err
	}
	hosts := []string{}
	valid := regexp.MustCompile(`^[A-Za-z0-9_.:%/-]{1,255}$`)
	for rows.Next() {
		var host string
		if err := rows.Scan(&host); err != nil {
			rows.Close()
			return err
		}
		if !valid.MatchString(host) {
			rows.Close()
			return errors.New("invalid existing local account host")
		}
		hosts = append(hosts, host)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	if len(hosts) == 0 || len(hosts) > 32 {
		return errors.New("bounded existing decision API accounts required")
	}
	for _, host := range hosts {
		if _, err := db.ExecContext(ctx, "GRANT EXECUTE ON FUNCTION `fraud`.`case_manager_visible_decision_inboxes_v1` TO `local_decision_api`@'"+host+"'"); err != nil {
			return err
		}
	}
	return nil
}
