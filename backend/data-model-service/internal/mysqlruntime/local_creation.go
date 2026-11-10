package mysqlruntime

import (
	"context"
	"crypto/sha256"
	"database/sql"
	sqldriver "database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/ec2test"
	"os"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

// LocalCreation runs only in the private development DDL process. The public
// API never receives its administrative socket or database credentials.
type LocalCreation struct {
	Target, Reference *sql.DB
	Plan              migrations.Plan
	Secret            string
}

func OpenLocalSocket(ctx context.Context, path, user, password string) (*sql.DB, error) {
	cfg := driver.NewConfig()
	cfg.Net = "unix"
	cfg.Addr = path
	cfg.User = user
	cfg.Passwd = password
	cfg.DBName = "fraud"
	cfg.ParseTime = true
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 30 * time.Second
	cfg.WriteTimeout = 30 * time.Second
	cfg.Params = map[string]string{"time_zone": "'+00:00'"}
	if err := ec2test.Driver(cfg, path == "/reference-socket/mysqld.sock"); err != nil {
		return nil, err
	}
	connector, err := driver.NewConnector(cfg)
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

func NewLocalCreation(ctx context.Context) (*LocalCreation, error) {
	root, err := os.ReadFile("/run/secrets/mysql_root_password")
	if err != nil {
		return nil, err
	}
	secret, err := os.ReadFile("/run/secrets/data_model_mysql_password")
	if err != nil {
		return nil, err
	}
	target, err := OpenLocalSocket(ctx, "/target-socket/mysqld.sock", "root", string(root))
	if err != nil {
		return nil, err
	}
	target.SetMaxOpenConns(8)
	reference, err := OpenLocalSocket(ctx, "/reference-socket/mysqld.sock", "root", string(root))
	if err != nil {
		target.Close()
		return nil, err
	}
	fail := func(err error) (*LocalCreation, error) { target.Close(); reference.Close(); return nil, err }
	raw, err := os.ReadFile("/plans/data_model.json")
	if err != nil {
		return fail(err)
	}
	var plan migrations.Plan
	if err := json.Unmarshal(raw, &plan); err != nil {
		return fail(err)
	}
	if plan.Database != "fraud" || plan.Owner != "data_model" {
		return fail(errors.New("local owner plan required"))
	}
	if err := plan.Validate(); err != nil {
		return fail(err)
	}
	var a, b string
	if err := target.QueryRowContext(ctx, "SELECT @@server_uuid").Scan(&a); err != nil {
		return fail(err)
	}
	if err := reference.QueryRowContext(ctx, "SELECT @@server_uuid").Scan(&b); err != nil {
		return fail(err)
	}
	if a == b {
		return fail(errors.New("separate reference server required"))
	}
	return &LocalCreation{target, reference, plan, string(secret)}, nil
}

func (p *LocalCreation) Create(ctx context.Context, input service.CreateTableInput, op uuid.UUID) (datamodel.Table, error) {
	var zero datamodel.Table
	if err := localAuthority(ctx, nil); err != nil {
		return zero, err
	}
	actor := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	if op == uuid.Nil {
		return zero, errors.New("operation identity required")
	}
	// Serialize retries of this operation without holding any row transaction
	// across reference-server calls or implicit-commit DDL.
	conn, err := p.Target.Conn(ctx)
	if err != nil {
		return zero, err
	}
	defer conn.Close()
	sum := sha256.Sum256([]byte(input.TenantID.String() + op.String()))
	lock := "dm_local_create_" + hex.EncodeToString(sum[:20])
	var acquired int
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,10)", lock).Scan(&acquired); err != nil {
		return zero, err
	}
	if acquired != 1 {
		return zero, errors.New("creation operation busy")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := conn.QueryRowContext(cleanup, "SELECT RELEASE_LOCK(?)", lock).Scan(&acquired); err != nil {
			conn.Raw(func(any) error { return sqldriver.ErrBadConn })
		}
	}()
	metadata, err := store.NewTransactionManager(p.Target, "fraud", input.TenantID, 1000, 4<<20, localTenantAuthority, nil)
	if err != nil {
		return zero, err
	}
	authorize := func(ctx context.Context, r ports.TableCreationRequest) error {
		if r.TenantID != input.TenantID || r.ActorID != actor || r.OperationID != op {
			return errors.New("creation scope mismatch")
		}
		return localAuthority(ctx, nil)
	}
	allocation, err := store.NewTableCreationManager(metadata, authorize)
	if err != nil {
		return zero, err
	}
	owner := service.NewTableService(nil, nil, nil, nil, nil, nil, nil, nil, localIDs{}, localClock{})
	saved, err := owner.PrepareCreation(ctx, input, op, actor, allocation)
	if err != nil {
		return zero, err
	}
	if record, err := owner.RecoverCreation(ctx, input, op, actor, allocation); err == nil {
		return record, nil
	} else if !errors.Is(err, store.ErrModelUnavailable) {
		return zero, err
	}
	table := saved.Snapshot.Table
	physical := store.PhysicalTable{TableID: table.ID}
	var fields []store.PhysicalField
	err = mysqlstore.Snapshot(ctx, p.Target, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, "SELECT physical_name FROM fraud.data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(input.TenantID), mysqlstore.UUID(table.ID)).Scan(&physical.Name); err != nil {
			return err
		}
		registry, err := store.NewPhysicalRegistry(tx, "fraud", input.TenantID, 1000)
		if err != nil {
			return err
		}
		fields, err = registry.Fields(ctx, table.ID)
		return err
	})
	if err != nil {
		return zero, err
	}
	mapped := make([]store.MappedPhysicalField, 0, 2)
	for _, field := range saved.Snapshot.Fields {
		m := store.MappedPhysicalField{Field: field}
		for _, v := range fields {
			if v.FieldID == field.ID {
				m.Components = append(m.Components, v)
			}
		}
		mapped = append(mapped, m)
	}
	suffix := strings.ReplaceAll(table.ID.String(), "-", "")[:24]
	guard := installation.Definer{User: "lcg_" + suffix, Host: "localhost"}
	account, _ := guard.SQLAccount()
	for _, db := range []*sql.DB{p.Target, p.Reference} {
		if _, err := db.ExecContext(ctx, "CREATE USER IF NOT EXISTS "+account+" ACCOUNT LOCK"); err != nil {
			return zero, err
		}
	}
	planner, err := store.NewPhysicalPlanner("fraud", input.TenantID, physical, mapped)
	if err != nil {
		return zero, err
	}
	creation, err := planner.CreateTableReference(saved.Snapshot.Fields[0].ID, saved.Snapshot.Fields[1].ID, guard)
	if err != nil {
		return zero, err
	}
	steps := creation.ReferenceSteps()
	ref, err := p.Reference.Conn(ctx)
	if err != nil {
		return zero, err
	}
	err = func() error {
		defer ref.Close()
		for i := range steps {
			after, err := migrations.Definition(ctx, ref, "fraud", steps[i].Kind, steps[i].Name)
			if err != nil {
				return err
			}
			if after == "" {
				if _, err := ref.ExecContext(ctx, steps[i].SQL); err != nil {
					return err
				}
				after, err = migrations.Definition(ctx, ref, "fraud", steps[i].Kind, steps[i].Name)
				if err != nil {
					return err
				}
			}
			steps[i].After = after
		}
		return nil
	}()
	if err != nil {
		return zero, err
	}
	plan, err := creation.Bind(steps)
	if err != nil {
		return zero, err
	}
	hash, err := service.TableCreationRequestHash(actor, input)
	if err != nil {
		return zero, err
	}
	if _, err := allocation.BindPlan(ctx, ports.TableCreationRequest{TenantID: input.TenantID, OperationID: op, ActorID: actor, RequestHash: hash}, plan); err != nil {
		return zero, err
	}
	installerHost, err := ec2test.InstallerHost()
	if err != nil {
		return zero, err
	}
	install := installation.RecordInstallerPolicy{Principal: installation.Definer{User: "lci_" + suffix, Host: installerHost}, Table: physical.Name}
	installer, _ := install.Principal.SQLAccount()
	passwordHash := sha256.Sum256([]byte(p.Secret + install.Principal.User))
	password := hex.EncodeToString(passwordHash[:])
	for _, q := range []string{"CREATE USER IF NOT EXISTS " + installer + " IDENTIFIED BY '" + password + "' ACCOUNT LOCK", "ALTER USER " + installer + " IDENTIFIED BY '" + password + "' ACCOUNT UNLOCK"} {
		if _, err := p.Target.ExecContext(ctx, q); err != nil {
			return zero, err
		}
	}
	// Cleanup also runs on failure. An interrupted process is recovered by the
	// same operation before publication; an active installer never passes readiness.
	deactivate := func(ctx context.Context) error {
		for _, q := range []string{"ALTER USER " + installer + " ACCOUNT LOCK", "REVOKE SET_ANY_DEFINER ON *.* FROM " + installer} {
			if _, err := p.Target.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}
	grants, err := install.ActiveGrants("fraud")
	if err != nil {
		return zero, err
	}
	executeErr := func() error {
		for _, q := range grants {
			if _, err := p.Target.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		login, err := OpenLocalSocket(ctx, "/target-socket/mysqld.sock", install.Principal.User, password)
		if err != nil {
			return err
		}
		defer login.Close()
		executor, err := store.NewPhysicalExecutor(login, "fraud", input.TenantID)
		if err != nil {
			return err
		}
		return executor.ExecuteSteps(ctx, table.ID, op)
	}()
	cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	cleanupErr := deactivate(cleanup)
	cancel()
	if err := errors.Join(executeErr, cleanupErr); err != nil {
		return zero, err
	}
	grants, err = installation.GrantStatements("fraud", creation.DefinerPolicy())
	if err != nil {
		return zero, err
	}
	for _, q := range grants {
		if _, err := p.Target.ExecContext(ctx, q); err != nil {
			return zero, err
		}
	}
	var policies []store.PhysicalAccessPolicy
	for _, mode := range []string{"writer", "ddl"} {
		user := "lcw_" + suffix
		privileges := []string{"SELECT", "INSERT", "UPDATE"}
		if mode == "ddl" {
			user = "lcd_" + suffix
			privileges = []string{"SELECT", "CREATE", "ALTER", "INDEX"}
		}
		d := installation.Definer{User: user, Host: "localhost"}
		a, _ := d.SQLAccount()
		pwd := sha256.Sum256([]byte(p.Secret + user))
		if _, err := p.Target.ExecContext(ctx, "CREATE USER IF NOT EXISTS "+a+" IDENTIFIED BY '"+hex.EncodeToString(pwd[:])+"'"); err != nil {
			return zero, err
		}
		policy := installation.AccountPolicy{User: user, Host: d.Host, Objects: []installation.ObjectGrant{{Kind: "TABLE", Name: physical.Name, Privileges: privileges}}}
		grants, err := installation.GrantStatements("fraud", policy)
		if err != nil {
			return zero, err
		}
		for _, q := range grants {
			if _, err := p.Target.ExecContext(ctx, q); err != nil {
				return zero, err
			}
		}
		policies = append(policies, store.PhysicalAccessPolicy{Mode: mode, Account: policy})
	}
	publication, err := store.NewPublicationStore(p.Target, "fraud", 1000, 1000, 4<<20)
	if err != nil {
		return zero, err
	}
	manager, err := store.NewPhysicalPublicationManager(metadata, publication, physical, policies, func(ctx context.Context, id uuid.UUID, r ports.PhysicalPublicationRequest) error {
		if id != input.TenantID || r.ActorID != actor.String() || r.TableID != table.ID || r.OperationID != op {
			return errors.New("publication scope mismatch")
		}
		return localAuthority(ctx, nil)
	})
	if err != nil {
		return zero, err
	}
	defs := []installation.Definer{}
	for _, name := range []string{"local_dm_guard", "local_dm_creation", "local_dm_result", "local_dm_record", "local_dm_contract"} {
		defs = append(defs, installation.Definer{User: name, Host: "localhost"})
	}
	ready, err := installation.NewExactKeyReadinessWithLookups(p.Target, p.Plan, defs[0], defs[1], defs[2], defs[3], defs[4])
	if err != nil {
		return zero, err
	}
	manager, err = manager.WithCreationReadiness(ready, install)
	if err != nil {
		return zero, err
	}
	if err := p.grantLocalConsumers(ctx, physical.Name); err != nil {
		return zero, err
	}
	request := ports.PhysicalPublicationRequest{TableID: table.ID, OperationID: op, RequestHash: saved.PublicationHash, Condition: store.PublicationCondition{Mode: store.PublicationMustBeAbsent}, ActorKind: "api", ActorID: actor.String()}
	if _, err := owner.PublishPreparedCreation(ctx, input, request, manager); err != nil {
		return zero, fmt.Errorf("publish creation: %w", err)
	}
	return owner.RecoverCreation(ctx, input, op, actor, allocation)
}
