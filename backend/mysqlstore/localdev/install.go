package localdev

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"github.com/go-sql-driver/mysql"
)

const Database = "fraud"

// Installation is local bootstrap configuration, never an application request.
// Definitions are captured on the separate reference server, then retained on
// disk and applied through the regular owner migration runner on both servers.
type Installation struct {
	Owner    string
	Steps    []migrations.Step
	Accounts []string
	Grants   []string
	Verify   func(context.Context, *sql.DB, migrations.Plan) error
	Upgrade  func(context.Context, *sql.DB, migrations.Plan) (migrations.Plan, error)
}

func socketDB(ctx context.Context, socket, password string) (*sql.DB, error) {
	c := mysql.NewConfig()
	c.Net, c.Addr, c.User, c.Passwd, c.DBName = "unix", socket, "root", password, Database
	c.ParseTime, c.Loc = true, time.UTC
	c.Timeout, c.ReadTimeout, c.WriteTimeout = 5*time.Second, 30*time.Second, 30*time.Second
	c.Params = map[string]string{"time_zone": "'+00:00'", "transaction_isolation": "'READ-COMMITTED'", "sql_mode": "'STRICT_TRANS_TABLES,ERROR_FOR_DIVISION_BY_ZERO,NO_ZERO_DATE,NO_ZERO_IN_DATE,NO_ENGINE_SUBSTITUTION,ONLY_FULL_GROUP_BY'"}
	connector, err := mysql.NewConnector(c)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func Accounts(ctx context.Context, db *sql.DB, names []string) error {
	for _, name := range names {
		if !strings.HasPrefix(name, "local_") || strings.ContainsAny(name, "`'\\\x00") || len(name) > 32 {
			return errors.New("invalid local definer account")
		}
		if _, err := db.ExecContext(ctx, "CREATE USER IF NOT EXISTS `"+name+"`@`localhost` ACCOUNT LOCK"); err != nil {
			return err
		}
		var locked string
		if err := db.QueryRowContext(ctx, "SELECT account_locked FROM mysql.user WHERE User=? AND Host='localhost'", name).Scan(&locked); err != nil {
			return err
		}
		if locked != "Y" {
			return errors.New("existing local definer is not locked")
		}
	}
	return nil
}

func matchingSource(plan migrations.Plan, installation Installation) error {
	if plan.Database != Database || plan.Owner != installation.Owner || len(plan.Steps) != len(installation.Steps) {
		return errors.New("local migration artifact differs from source")
	}
	for i, step := range installation.Steps {
		got := plan.Steps[i]
		if step.ID != got.ID || step.Kind != got.Kind || step.Name != got.Name || step.SQL != got.SQL {
			return fmt.Errorf("local migration source changed at %s; an explicit upgrade artifact is required", step.ID)
		}
	}
	return plan.Validate()
}

func Install(ctx context.Context, installation Installation) error {
	if _, err := Origin(os.Getenv("LOCAL_MYSQL_NO_AUTH"), os.Getenv("APP_ORIGIN")); err != nil {
		return err
	}
	if installation.Owner != "data_model" && installation.Owner != "ingestion" && installation.Owner != "decision_engine" && installation.Owner != "identity" && installation.Owner != "identity_auth" && installation.Owner != "case_manager" && installation.Owner != "screening" && installation.Owner != "decision_local" {
		return errors.New("unsupported local owner")
	}
	password, err := os.ReadFile("/run/secrets/mysql_root_password")
	if err != nil {
		return err
	}
	target, err := socketDB(ctx, "/target-socket/mysqld.sock", string(password))
	if err != nil {
		return err
	}
	defer target.Close()
	reference, err := socketDB(ctx, "/reference-socket/mysqld.sock", string(password))
	if err != nil {
		return err
	}
	defer reference.Close()
	var targetID, referenceID, version string
	if err := target.QueryRowContext(ctx, "SELECT @@server_uuid,VERSION()").Scan(&targetID, &version); err != nil {
		return err
	}
	if version != "8.4.8" {
		return errors.New("pinned MySQL 8.4.8 required")
	}
	if err := reference.QueryRowContext(ctx, "SELECT @@server_uuid,VERSION()").Scan(&referenceID, &version); err != nil {
		return err
	}
	if version != "8.4.8" || targetID == referenceID {
		return errors.New("separate pinned reference MySQL server required")
	}
	for _, db := range []*sql.DB{reference, target} {
		if err := Accounts(ctx, db, installation.Accounts); err != nil {
			return err
		}
	}
	path := filepath.Join("/plans", installation.Owner+".json")
	var plan migrations.Plan
	captured := false
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &plan); err != nil {
			return err
		}
		if err := matchingSource(plan, installation); err != nil {
			if installation.Upgrade == nil || len(plan.Steps) >= len(installation.Steps) {
				return err
			}
			prefix := installation
			prefix.Steps = installation.Steps[:len(plan.Steps)]
			if err := matchingSource(plan, prefix); err != nil {
				return err
			}
			upgraded, err := installation.Upgrade(ctx, reference, plan)
			if err != nil {
				return err
			}
			if len(upgraded.Steps) < len(plan.Steps) || !reflect.DeepEqual(upgraded.Steps[:len(plan.Steps)], plan.Steps) {
				return errors.New("upgrade rewrote retained migration prefix")
			}
			if err := matchingSource(upgraded, installation); err != nil {
				return err
			}
			// Retain every upgrade's exact input. A single fixed backup name
			// prevents a second legitimate append-only owner upgrade.
			backup := fmt.Sprintf("%s.before-upgrade-%x", path, sha256.Sum256(data))
			f, e := os.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if e == nil {
				_, writeErr := f.Write(data)
				if e = errors.Join(writeErr, f.Sync(), f.Close()); e != nil {
					return e
				}
			} else if errors.Is(e, os.ErrExist) {
				old, e := os.ReadFile(backup)
				if e != nil || !bytes.Equal(old, data) {
					return errors.New("existing upgrade backup differs from installed artifact")
				}
			} else {
				return e
			}
			updated, err := json.MarshalIndent(upgraded, "", "  ")
			if err != nil {
				return err
			}
			tmp, err := os.CreateTemp(filepath.Dir(path), ".owner-upgrade-*")
			if err != nil {
				return err
			}
			defer os.Remove(tmp.Name())
			_, writeErr := tmp.Write(updated)
			if err := errors.Join(writeErr, tmp.Sync(), tmp.Close()); err != nil {
				return err
			}
			if err := os.Rename(tmp.Name(), path); err != nil {
				return err
			}
			plan = upgraded
		}
	} else if errors.Is(err, os.ErrNotExist) {
		captured = true
		plan = migrations.Plan{Database: Database, Owner: installation.Owner, Steps: append([]migrations.Step(nil), installation.Steps...)}
		conn, err := reference.Conn(ctx)
		if err != nil {
			return err
		}
		defer conn.Close()
		for i := range plan.Steps {
			step := &plan.Steps[i]
			if strings.HasPrefix(step.SQL, "ALTER ") {
				step.Before, err = migrations.Definition(ctx, conn, Database, step.Kind, step.Name)
				if err != nil {
					return fmt.Errorf("reference before %s: %w", step.ID, err)
				}
			}
			if _, err := conn.ExecContext(ctx, step.SQL); err != nil {
				return fmt.Errorf("reference %s: %w", step.ID, err)
			}
			step.After, err = migrations.Definition(ctx, conn, Database, step.Kind, step.Name)
			if err != nil {
				return fmt.Errorf("reference after %s: %w", step.ID, err)
			}
		}
		if err := matchingSource(plan, installation); err != nil {
			return err
		}
		data, err = json.MarshalIndent(plan, "", "  ")
		if err != nil {
			return err
		}
		// No replacement of an earlier plan: restart uses the same definitions.
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, writeErr := f.Write(data)
		if err := errors.Join(writeErr, f.Sync(), f.Close()); err != nil {
			return err
		}
	} else {
		return err
	}
	for _, db := range []*sql.DB{reference, target} {
		if db == reference && captured {
			for _, grant := range installation.Grants {
				if _, err := db.ExecContext(ctx, grant); err != nil {
					return err
				}
			}
			continue
		}
		if db == reference {
			conn, err := db.Conn(ctx)
			if err != nil {
				return err
			}
			latest := map[string]migrations.Step{}
			for _, step := range plan.Steps {
				latest[step.Kind+":"+step.Name] = step
			}
			present := 0
			for _, step := range latest {
				actual, err := migrations.Definition(ctx, conn, Database, step.Kind, step.Name)
				if err != nil {
					conn.Close()
					return err
				}
				if actual != "" {
					if actual != step.After {
						conn.Close()
						return migrations.ErrDrift
					}
					present++
				}
			}
			conn.Close()
			if present == len(latest) {
				for _, grant := range installation.Grants {
					if _, err := db.ExecContext(ctx, grant); err != nil {
						return err
					}
				}
				continue
			}
			if present != 0 {
				return errors.New("partial reference schema; recreate the disposable reference container")
			}
		}
		runner, err := migrations.NewForOwner(db, installation.Owner, plan)
		if err != nil {
			return err
		}
		if err := runner.Apply(ctx); err != nil {
			return err
		}
		for _, grant := range installation.Grants {
			if _, err := db.ExecContext(ctx, grant); err != nil {
				return err
			}
		}
	}
	if installation.Verify != nil {
		return installation.Verify(ctx, target, plan)
	}
	return nil
}
