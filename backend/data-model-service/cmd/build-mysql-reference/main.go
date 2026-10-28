// build-mysql-reference creates reviewable installation artifacts exclusively
// against the isolated Compose reference server. It is not a production command.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("build-mysql-reference", flag.ContinueOnError)
	flags.SetOutput(out)
	database := flags.String("database", "", "shared target database name, created only on the isolated reference server")
	user := flags.String("definer-user", "data_model_guard", "dedicated locked guard identity")
	host := flags.String("definer-host", "localhost", "guard account host")
	directory := flags.String("output", "", "new artifact directory; existing paths are never overwritten")
	retryUpgrade := flags.Bool("delivery-retry-upgrade", false, "append the reviewed v002 delivery error-count upgrade to v001")
	receiptUser := flags.String("creation-receipt-definer-user", "", "append v002/v003 using a separate locked receipt definer at definer-host")
	resultUser := flags.String("owner-result-definer-user", "", "append v004; requires a separate creation-receipt-definer-user")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 || *database == "" || *directory == "" {
		return errors.New("database and new output directory required")
	}
	if _, err := mysqlstore.Identifier(*database); err != nil {
		return err
	}
	switch *database {
	case "mysql", "sys", "information_schema", "performance_schema", "data_model_publication_test":
		return errors.New("reserved reference database")
	}
	definer := installation.Definer{User: *user, Host: *host}
	var receiptDefiners []installation.Definer
	if *receiptUser != "" {
		d := installation.Definer{User: *receiptUser, Host: *host}
		if _, err := d.SQLAccount(); err != nil {
			return err
		}
		if d.User == definer.User {
			return errors.New("receipt definer must differ from shared definer")
		}
		receiptDefiners = append(receiptDefiners, d)
	}
	if *resultUser != "" {
		if len(receiptDefiners) != 1 || *resultUser == definer.User || *resultUser == receiptDefiners[0].User {
			return errors.New("v004 requires distinct shared, creation and result definers")
		}
		d := installation.Definer{User: *resultUser, Host: *host}
		if _, err := d.SQLAccount(); err != nil {
			return err
		}
		receiptDefiners = append(receiptDefiners, d)
	}
	if _, err := installation.Statements(*database, definer); err != nil {
		return err
	}
	if _, err := os.Stat(*directory); !errors.Is(err, os.ErrNotExist) {
		return errors.New("output must be a new directory")
	}
	if os.Getenv("DATA_MODEL_REQUIRE_MYSQL_TEST") != "1" || os.Getenv("DATA_MODEL_MYSQL_TEST_ADDRESS") != "mysql:3306" || os.Getenv("DATA_MODEL_MYSQL_TEST_ROOT_PASSWORD") != "isolated-test-root-only" {
		return errors.New("only the explicitly configured disposable Compose fixture is permitted")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	plan, err := reference(ctx, *database, definer, *retryUpgrade || len(receiptDefiners) > 0, receiptDefiners...)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > 32<<20 {
		return errors.New("reference artifact exceeds migration-command bound")
	}
	create, err := installation.CreateDefiner(definer)
	if err != nil {
		return err
	}
	grants, err := installation.DefinerGrants(*database, definer)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	provenance, err := json.MarshalIndent(struct {
		ServerVersion   string                 `json:"server_version"`
		Database        string                 `json:"database"`
		Definer         installation.Definer   `json:"definer"`
		PlanSHA256      string                 `json:"plan_sha256"`
		ReviewStatus    string                 `json:"review_status"`
		ReceiptDefiners []installation.Definer `json:"receipt_definers,omitempty"`
	}{"8.4.8", *database, definer, hex.EncodeToString(sum[:]), "needs_review", receiptDefiners}, "", "  ")
	if err != nil {
		return err
	}
	// Reference cleanup has completed before artifact publication. Creating a
	// new directory prevents a later build from rewriting an installed artifact.
	if err := os.Mkdir(*directory, 0700); err != nil {
		return err
	}
	for _, file := range []struct {
		name string
		data []byte
	}{
		{"plan.json", data},
		{"create_definer.sql", []byte(create + ";\n")},
		{"grant_definer.sql", []byte(strings.Join(grants, ";\n") + ";\n")},
		{"provenance.json", append(provenance, '\n')},
	} {
		if err := os.WriteFile(filepath.Join(*directory, file.name), file.data, 0600); err != nil {
			return err
		}
	}
	for i, receiptDefiner := range receiptDefiners {
		create, err := installation.CreateDefiner(receiptDefiner)
		if err != nil {
			return err
		}
		policy := installation.CreationReceiptDefinerPolicy(receiptDefiner)
		prefix := "receipt"
		if i == 1 {
			policy = installation.OwnerResultDefinerPolicy(receiptDefiner)
			prefix = "owner_result"
		}
		grants, err := installation.GrantStatements(*database, policy)
		if err != nil {
			return err
		}
		for file, value := range map[string]string{"create_" + prefix + "_definer.sql": create + ";\n", "grant_" + prefix + "_definer.sql": strings.Join(grants, ";\n") + ";\n"} {
			if err := os.WriteFile(filepath.Join(*directory, file), []byte(value), 0600); err != nil {
				return err
			}
		}
	}
	_, err = fmt.Fprintf(out, "reference artifact: database=%s steps=%d sha256=%s; review required; no application database inspected\n", *database, len(plan.Steps), hex.EncodeToString(sum[:]))
	return err
}

func reference(ctx context.Context, database string, definer installation.Definer, retryUpgrade bool, receiptDefiners ...installation.Definer) (plan migrations.Plan, result error) {
	if len(receiptDefiners) > 2 || (len(receiptDefiners) > 0 && (!retryUpgrade || receiptDefiners[0].User == definer.User)) {
		return plan, errors.New("v003 requires v002 and a separate receipt definer")
	}
	if len(receiptDefiners) == 2 && (receiptDefiners[1].User == definer.User || receiptDefiners[1].User == receiptDefiners[0].User) {
		return plan, errors.New("distinct owner-result definer required")
	}
	db, err := mysqlstore.Open(ctx, mysqlstore.Config{Address: "mysql:3306", Database: "data_model_publication_test", User: "root", Password: "isolated-test-root-only", AllowInsecureTransport: true, MaxOpen: 2, MaxIdle: 1, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if err != nil {
		return plan, err
	}
	defer func() {
		result = errors.Join(result, db.Close())
		if result != nil {
			plan = migrations.Plan{}
		}
	}()
	var version string
	if err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version); err != nil {
		return plan, err
	}
	if version != "8.4.8" {
		return plan, errors.New("reference server must be exactly MySQL 8.4.8")
	}
	schema, _ := mysqlstore.Identifier(database)
	create, err := installation.CreateDefiner(definer)
	if err != nil {
		return plan, err
	}
	account, err := definer.SQLAccount()
	if err != nil {
		return plan, err
	}
	createdDatabase, createdAccount := false, false
	var receiptAccounts []string
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		if createdDatabase {
			_, err := db.ExecContext(cleanup, "DROP DATABASE "+schema)
			result = errors.Join(result, err)
		}
		if createdAccount {
			_, err := db.ExecContext(cleanup, "DROP USER "+account)
			result = errors.Join(result, err)
		}
		for _, receiptAccount := range receiptAccounts {
			_, err := db.ExecContext(cleanup, "DROP USER "+receiptAccount)
			result = errors.Join(result, err)
		}
		if result != nil {
			plan = migrations.Plan{}
		}
	}()
	// CREATE without IF NOT EXISTS prevents adoption/deletion of existing data or
	// accounts. Only successfully created reference objects are cleaned up.
	if _, err := db.ExecContext(ctx, "CREATE DATABASE "+schema+" CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin"); err != nil {
		return plan, err
	}
	createdDatabase = true
	if _, err := db.ExecContext(ctx, create); err != nil {
		return plan, err
	}
	createdAccount = true
	for _, receiptDefiner := range receiptDefiners {
		create, err := installation.CreateDefiner(receiptDefiner)
		if err != nil {
			return plan, err
		}
		if _, err := db.ExecContext(ctx, create); err != nil {
			return plan, err
		}
		receiptAccount, _ := receiptDefiner.SQLAccount()
		receiptAccounts = append(receiptAccounts, receiptAccount)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return plan, err
	}
	defer func() { result = errors.Join(result, conn.Close()) }()
	steps, err := installation.Statements(database, definer)
	if err != nil {
		return plan, err
	}
	for i := range steps {
		step := &steps[i]
		step.Before, err = migrations.Definition(ctx, conn, database, step.Kind, step.Name)
		if err != nil {
			return plan, err
		}
		if _, err := conn.ExecContext(ctx, step.SQL); err != nil {
			return plan, fmt.Errorf("reference step %s: %w", step.ID, err)
		}
		step.After, err = migrations.Definition(ctx, conn, database, step.Kind, step.Name)
		if err != nil {
			return plan, err
		}
	}
	grants, err := installation.DefinerGrants(database, definer)
	if err != nil {
		return plan, err
	}
	for _, grant := range grants {
		if _, err := conn.ExecContext(ctx, grant); err != nil {
			return plan, err
		}
	}
	if err := installation.VerifyDefiner(ctx, conn, database, definer); err != nil {
		return plan, err
	}
	plan, err = installation.Plan(database, definer, steps)
	if err != nil || !retryUpgrade {
		return plan, err
	}
	upgrade, err := installation.DeliveryRetryUpgradeStatement(database)
	if err != nil {
		return migrations.Plan{}, err
	}
	upgrade.Before, err = migrations.Definition(ctx, conn, database, upgrade.Kind, upgrade.Name)
	if err != nil {
		return migrations.Plan{}, err
	}
	if _, err = conn.ExecContext(ctx, upgrade.SQL); err != nil {
		return migrations.Plan{}, err
	}
	upgrade.After, err = migrations.Definition(ctx, conn, database, upgrade.Kind, upgrade.Name)
	if err != nil {
		return migrations.Plan{}, err
	}
	plan, err = installation.ExtendDeliveryRetryPlan(plan, upgrade)
	if err != nil || len(receiptDefiners) == 0 {
		return plan, err
	}
	for i, receiptDefiner := range receiptDefiners {
		steps, err = installation.CreationReceiptUpgradeStatements(database, receiptDefiner)
		if i == 1 {
			steps, err = installation.OwnerResultUpgradeStatements(database, receiptDefiner)
		}
		if err != nil {
			return migrations.Plan{}, err
		}
		for i := range steps {
			s := &steps[i]
			s.Before, err = migrations.Definition(ctx, conn, database, s.Kind, s.Name)
			if err != nil {
				return migrations.Plan{}, err
			}
			if _, err = conn.ExecContext(ctx, s.SQL); err != nil {
				return migrations.Plan{}, err
			}
			s.After, err = migrations.Definition(ctx, conn, database, s.Kind, s.Name)
			if err != nil {
				return migrations.Plan{}, err
			}
		}
		policy := installation.CreationReceiptDefinerPolicy(receiptDefiner)
		if i == 1 {
			policy = installation.OwnerResultDefinerPolicy(receiptDefiner)
		}
		grants, err = installation.GrantStatements(database, policy)
		if err != nil {
			return migrations.Plan{}, err
		}
		for _, grant := range grants {
			if _, err = conn.ExecContext(ctx, grant); err != nil {
				return migrations.Plan{}, err
			}
		}
		if err := installation.VerifyAccountPolicy(ctx, conn, database, policy); err != nil {
			return migrations.Plan{}, err
		}
		if i == 0 {
			plan, err = installation.ExtendCreationReceiptPlan(plan, receiptDefiner, steps)
		} else {
			plan, err = installation.ExtendOwnerResultPlan(plan, receiptDefiner, steps)
		}
		if err != nil {
			return migrations.Plan{}, err
		}
	}
	return plan, nil
}
