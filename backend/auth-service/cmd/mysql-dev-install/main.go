package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/internal/mysqlauth"
	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/mysqlidentity"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/localdev"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"github.com/google/uuid"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, "auth install failed:", e)
		os.Exit(1)
	}
}
func run() error {
	provision := flag.String("provision", "", "private JSON operator provisioning file; never an HTTP registration route")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	steps, e := mysqlidentity.Statements("fraud", "local_identity_guard")
	if e != nil {
		return e
	}
	grants, e := mysqlidentity.DefinerGrants("fraud", "local_identity_guard")
	if e != nil {
		return e
	}
	if e = localdev.Install(ctx, localdev.Installation{Owner: "identity", Steps: steps, Accounts: []string{"local_identity_guard"}, Grants: grants}); e != nil {
		return e
	}
	steps, e = mysqlauth.Statements("fraud")
	if e != nil {
		return e
	}
	return localdev.Install(ctx, localdev.Installation{Owner: mysqlauth.Owner, Steps: steps, Verify: func(ctx context.Context, db *sql.DB, p migrations.Plan) error {
		c, e := mysqlauth.LoadConfig("/runtime/config.json")
		if e != nil {
			return e
		}
		parts := strings.Split(c.AccountHost, "/")
		if len(parts) != 2 || net.ParseIP(parts[0]).To4() == nil || net.ParseIP(parts[1]).To4() == nil {
			return errors.New("explicit local Docker IPv4 account network required")
		}
		// Provisioning can run while auth is serving traffic. Only an explicit
		// installation refreshes account grants, with API/worker stopped by setup.
		if *provision == "" {
			for _, worker := range []bool{false, true} {
				role := "api"
				if worker {
					role = "worker"
				}
				user := "local_auth_" + role
				raw, e := os.ReadFile(filepath.Join("/run/secrets", "auth_"+role+"_password"))
				if e != nil {
					return e
				}
				password := string(raw)
				if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(password) {
					return errors.New("invalid generated auth password")
				}
				account := "`" + user + "`@'" + c.AccountHost + "'"
				for _, query := range []string{"CREATE USER IF NOT EXISTS " + account + " IDENTIFIED BY '" + password + "'", "ALTER USER " + account + " IDENTIFIED BY '" + password + "'", "REVOKE ALL PRIVILEGES, GRANT OPTION FROM " + account} {
					if _, e = db.ExecContext(ctx, query); e != nil {
						return errors.New("auth account configuration failed")
					}
				}
				grants, e := mysqlauth.RuntimeGrants("fraud", user, worker)
				if e != nil {
					return e
				}
				for _, g := range grants {
					g = strings.ReplaceAll(g, "`"+user+"`@'%'", account)
					if _, e = db.ExecContext(ctx, g); e != nil {
						return e
					}
				}
			}
		}
		if *provision != "" {
			f, e := os.Open(*provision)
			if e != nil {
				return e
			}
			defer f.Close()
			var spec struct {
				TenantID              uuid.UUID
				Issuer, Subject, Name string
				Roles                 []string
			}
			d := json.NewDecoder(io.LimitReader(f, 16385))
			d.DisallowUnknownFields()
			if e = d.Decode(&spec); e != nil {
				return e
			}
			var extra any
			if d.Decode(&extra) != io.EOF {
				return errors.New("one provisioning command required")
			}
			var active bool
			if e = db.QueryRowContext(ctx, "SELECT status='active' FROM fraud.data_model_tenants WHERE id=?", mysqlstore.UUID(spec.TenantID)).Scan(&active); e != nil {
				return e
			}
			if !active {
				return errors.New("provisioning requires an active registered tenant")
			}
			s, e := mysqlauth.New(db, "fraud")
			if e != nil {
				return e
			}
			if e = s.RegisterTenant(ctx, spec.TenantID); e != nil {
				return e
			}
			user, e := s.Provision(ctx, spec.TenantID, spec.Issuer, spec.Subject, spec.Name, spec.Roles)
			if e != nil {
				return e
			}
			fmt.Println("auth provisioned tenant", spec.TenantID, "user", user)
		}
		fmt.Println("auth MySQL migrations verified; application authentication remains enabled")
		return nil
	}})
}
