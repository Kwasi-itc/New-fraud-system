// ec2-test-preflight checks the remote test target before owner migrations run.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/ec2test"
)

func run() error {
	initialize := flag.Bool("initialize", false, "create the shared fraud database if absent after checks pass")
	flag.Parse()
	c, err := ec2test.Load()
	if err != nil {
		return err
	}
	password, err := os.ReadFile("/run/secrets/mysql_root_password")
	if err != nil {
		return errors.New("migration credential is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := ec2test.Open(ctx, mysqlstore.Config{Address: c.Address, Database: "information_schema", User: c.AdminUser, Password: string(password), MaxOpen: 1, MaxIdle: 1, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if err != nil {
		return errors.New("Aurora connection failed; check endpoint, CA, credentials and EC2 network access")
	}
	defer db.Close()
	var version, aurora string
	var readOnly, trust, maxConnections int
	if err := db.QueryRowContext(ctx, "SELECT VERSION(),AURORA_VERSION(),@@read_only,@@log_bin_trust_function_creators,@@max_connections").Scan(&version, &aurora, &readOnly, &trust, &maxConnections); err != nil {
		return errors.New("target is not a verified Aurora MySQL writer")
	}
	if !ec2test.VersionSupported(version) {
		return fmt.Errorf("Aurora reports MySQL %s; this test profile requires MySQL 8.4 compatibility", version)
	}
	if readOnly != 0 {
		return errors.New("a writable Aurora writer endpoint is required")
	}
	if trust != 1 {
		return errors.New("set log_bin_trust_function_creators=1 in the Aurora parameter group before running restricted trigger installers")
	}
	// Single-replica configured runtime pools total 100 connections. Reserve
	// additional capacity for temporary DDL sessions, migrations and operators.
	if maxConnections < 140 {
		return errors.New("max_connections must be at least 140 for the configured runtime pools and migration headroom; account for other clients separately")
	}
	fmt.Printf("Aurora %s / MySQL %s: writer connectivity and verified TLS passed\n", aurora, version)
	if *initialize {
		_, err = db.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS fraud CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin")
	}
	return err
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
