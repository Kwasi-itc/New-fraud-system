package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/localdev"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	R "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/mysqlruntime"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/mysqlscreening"
	"net"
	"os"
	"strings"
	"time"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, "screening install failed:", e)
		os.Exit(1)
	}
}
func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	steps, e := mysqlscreening.Statements("fraud", "local_screening_guard")
	if e != nil {
		return e
	}
	grants, e := mysqlscreening.DefinerGrants("fraud", "local_screening_guard")
	if e != nil {
		return e
	}
	return localdev.Install(ctx, localdev.Installation{Owner: "screening", Steps: steps, Accounts: []string{"local_screening_guard"}, Grants: grants, Upgrade: extendLocalOwnerPlan(steps), Verify: configure})
}
func configure(ctx context.Context, db *sql.DB, _ migrations.Plan) error {
	c, e := R.Load("/runtime/config.json")
	if e != nil {
		return e
	}
	parts := strings.Split(c.AccountHost, "/")
	if len(parts) != 2 || net.ParseIP(parts[0]).To4() == nil || net.ParseIP(parts[1]).To4() == nil {
		return errors.New("explicit Docker IPv4 account network required")
	}
	for _, role := range []string{"api", "worker"} {
		password, e := R.Secret("screening_" + role + "_password")
		if e != nil {
			return e
		}
		account := "`local_screening_" + role + "`@'" + c.AccountHost + "'"
		for _, q := range []string{"CREATE USER IF NOT EXISTS " + account + " IDENTIFIED BY '" + password + "'", "ALTER USER " + account + " IDENTIFIED BY '" + password + "'", "REVOKE ALL PRIVILEGES, GRANT OPTION FROM " + account} {
			if _, e = db.ExecContext(ctx, q); e != nil {
				return errors.New("screening account configuration failed")
			}
		}
		if e = R.ConfigureRuntimeGrants(ctx, db, "fraud", role, "local_screening_"+role, c.AccountHost); e != nil {
			return e
		}
	}
	fmt.Println("screening owner upgrade and restricted local accounts verified")
	return nil
}
