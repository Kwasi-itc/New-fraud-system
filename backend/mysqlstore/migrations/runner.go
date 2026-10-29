package migrations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	driver "github.com/go-sql-driver/mysql"
)

var ErrDrift = errors.New("migration object definition differs from reviewed plan")
var ErrLineage = errors.New("migration history differs from supplied ordered checksums")

const ledgerCreate = "CREATE TABLE `data_model_schema_migrations` (\n" +
	"  `step_no` int NOT NULL,\n" +
	"  `step_id` varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,\n" +
	"  `checksum` binary(32) NOT NULL,\n" +
	"  `state` varchar(8) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,\n" +
	"  `started_at` datetime(6) NOT NULL,\n" +
	"  `applied_at` datetime(6) DEFAULT NULL,\n" +
	"  PRIMARY KEY (`step_no`),\n" +
	"  UNIQUE KEY `step_id` (`step_id`),\n" +
	"  CONSTRAINT `data_model_migration_state` CHECK (((`state` = _ascii'dirty') or (`state` = _ascii'applied')))\n" +
	") ENGINE=InnoDB DEFAULT CHARSET=ascii COLLATE=ascii_bin"

// Definition returns canonical JSON of the entire SHOW CREATE row, including
// routine SQL mode and character-set context. Empty means absent, not ready.
// Its connection needs installer-level metadata visibility. This is diagnostic
// deployment code; never expose physical names or definitions to API callers.
func Definition(ctx context.Context, conn *sql.Conn, database, kind, name string) (string, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return "", err
	}
	object, err := mysqlstore.Identifier(name)
	if err != nil {
		return "", err
	}
	switch kind {
	case "TABLE", "VIEW", "PROCEDURE", "FUNCTION", "TRIGGER":
	default:
		return "", errors.New("unsupported object kind")
	}
	rows, err := conn.QueryContext(ctx, "SHOW CREATE "+kind+" "+schema+"."+object)
	if err != nil {
		var e *driver.MySQLError
		if errors.As(err, &e) && (e.Number == 1146 || e.Number == 1305 || e.Number == 1360) {
			return "", nil
		}
		return "", err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return "", err
	}
	if !rows.Next() {
		return "", errors.Join(errors.New("missing SHOW CREATE result"), rows.Err())
	}
	values := make([]sql.NullString, len(columns))
	args := make([]any, len(columns))
	for i := range values {
		args[i] = &values[i]
	}
	if err := rows.Scan(args...); err != nil {
		return "", err
	}
	result := make(map[string]string, len(columns))
	for i, name := range columns {
		if !values[i].Valid {
			return "", errors.New("incomplete SHOW CREATE visibility")
		}
		// Trigger creation timestamps are not part of its executable definition.
		if name != "Created" {
			result[name] = values[i].String
		}
	}
	if rows.Next() {
		return "", errors.New("multiple SHOW CREATE rows")
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	data, err := json.Marshal(result)
	if len(data) > 1<<20 {
		return "", errors.New("migration definition exceeds 1 MiB")
	}
	return string(data), err
}

type History struct {
	Number   int
	ID       string
	Checksum []byte
	State    string
}

type Runner struct {
	db           *sql.DB
	plan         Plan
	owner        string
	ledger       string
	ledgerCreate string
}

// New binds a reviewed owner plan to its independently tracked ledger. Owner is
// a fixed deployment identity, never a caller/tenant-provided table prefix.
func New(db *sql.DB, owner string, plan Plan) (*Runner, error) {
	if db == nil {
		return nil, errors.New("migration database required")
	}
	ledger := owner + "_schema_migrations"
	if _, err := mysqlstore.Identifier(ledger); err != nil || len(owner) == 0 {
		return nil, errors.New("bounded owner migration namespace required")
	}
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	for _, step := range plan.Steps {
		if step.Name == ledger {
			return nil, errors.New("migration plan cannot mutate its own ledger")
		}
	}
	plan.Steps = append([]Step(nil), plan.Steps...)
	// Preserve data-model's shipped definition and step checksums byte-for-byte.
	definition := strings.ReplaceAll(ledgerCreate, "data_model_schema_migrations", ledger)
	definition = strings.ReplaceAll(definition, "data_model_migration_state", owner+"_migration_state")
	return &Runner{db: db, plan: plan, owner: owner, ledger: ledger, ledgerCreate: definition}, nil
}

func (r *Runner) locked(ctx context.Context, fn func(*sql.Conn, string) error) error {
	schema, _ := mysqlstore.Identifier(r.plan.Database)
	hash := sha256.Sum256([]byte(r.plan.Database))
	// Hash both identities to keep the named lock within MySQL's 64-byte limit.
	// Retain the old data-model lock name for already-shipped installers.
	lock := fmt.Sprintf("data_model_migrate_%x", hash[:22])
	if r.owner != "data_model" {
		hash = sha256.Sum256([]byte(r.plan.Database + ":" + r.owner))
		lock = fmt.Sprintf("owner_migrate_%x", hash[:24])
	}
	return mysqlstore.WithDDLLock(ctx, r.db, lock, func(conn *sql.Conn) error {
		// All DDL, probes and autocommit journal writes use this same session.
		return fn(conn, schema)
	})
}

func (r *Runner) bootstrap(ctx context.Context, conn *sql.Conn, schema string) error {
	actual, err := Definition(ctx, conn, r.plan.Database, "TABLE", r.ledger)
	if err != nil {
		return err
	}
	if actual == "" {
		statement := "CREATE TABLE " + schema + "." + r.ledgerCreate[len("CREATE TABLE "):]
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create migration ledger (outcome requires inspection): %w", err)
		}
		actual, err = Definition(ctx, conn, r.plan.Database, "TABLE", r.ledger)
		if err != nil {
			return err
		}
	}
	want, _ := json.Marshal(map[string]string{"Table": r.ledger, "Create Table": r.ledgerCreate})
	if actual != string(want) {
		return fmt.Errorf("migration ledger: %w", ErrDrift)
	}
	return nil
}

func (r *Runner) history(ctx context.Context, conn *sql.Conn, schema string) ([]History, error) {
	rows, err := conn.QueryContext(ctx, "SELECT step_no,step_id,checksum,state FROM "+schema+".`"+r.ledger+"` ORDER BY step_no LIMIT ?", len(r.plan.Steps)+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []History
	for rows.Next() {
		var h History
		if err := rows.Scan(&h.Number, &h.ID, &h.Checksum, &h.State); err != nil {
			return nil, err
		}
		i := len(result)
		if i >= len(r.plan.Steps) {
			return nil, ErrLineage
		}
		want := checksum(r.plan.Database, r.plan.Steps[i])
		if h.Number != i+1 || h.ID != r.plan.Steps[i].ID || !bytes.Equal(h.Checksum, want[:]) || (h.State != "dirty" && h.State != "applied") || (i > 0 && result[i-1].State != "applied") {
			return nil, ErrLineage
		}
		result = append(result, h)
	}
	return result, rows.Err()
}

// Status verifies the ledger and checksums but does not execute migration DDL.
// A database without a ledger is reported as an empty history without creating it.
func (r *Runner) Status(ctx context.Context) ([]History, error) {
	var result []History
	err := r.locked(ctx, func(conn *sql.Conn, schema string) error {
		definition, err := Definition(ctx, conn, r.plan.Database, "TABLE", r.ledger)
		if err != nil || definition == "" {
			return err
		}
		if err := r.bootstrap(ctx, conn, schema); err != nil {
			return err
		}
		result, err = r.history(ctx, conn, schema)
		return err
	})
	return result, err
}

// Apply resumes a dirty step only when its object exactly matches Before or
// After. An existing object without a journal is never silently adopted. Errors
// leave dirty state visible; there is no force-clean or destructive down command.
func (r *Runner) Apply(ctx context.Context) error {
	return r.locked(ctx, func(conn *sql.Conn, schema string) error {
		if err := r.bootstrap(ctx, conn, schema); err != nil {
			return err
		}
		history, err := r.history(ctx, conn, schema)
		if err != nil {
			return err
		}
		// Verify the latest applied shape for each object. If the last step is
		// dirty for that object its before/after inspection below supersedes it.
		latest := make(map[string]int)
		for i := range history {
			s := r.plan.Steps[i]
			latest[s.Kind+":"+s.Name] = i
		}
		for i, h := range history {
			s := r.plan.Steps[i]
			if latest[s.Kind+":"+s.Name] != i || h.State != "applied" {
				continue
			}
			actual, err := Definition(ctx, conn, r.plan.Database, s.Kind, s.Name)
			if err != nil {
				return err
			}
			if actual != s.After {
				return fmt.Errorf("applied step %s: %w", s.ID, ErrDrift)
			}
		}
		for i, s := range r.plan.Steps {
			if i < len(history) && history[i].State == "applied" {
				continue
			}
			actual, err := Definition(ctx, conn, r.plan.Database, s.Kind, s.Name)
			if err != nil {
				return err
			}
			dirty := i < len(history)
			if actual != s.Before && !(dirty && actual == s.After) {
				return fmt.Errorf("step %s: %w", s.ID, ErrDrift)
			}
			sum := checksum(r.plan.Database, s)
			if !dirty {
				if _, err := conn.ExecContext(ctx, "INSERT INTO "+schema+".`"+r.ledger+"`(step_no,step_id,checksum,state,started_at) VALUES (?,?,?,'dirty',UTC_TIMESTAMP(6))", i+1, s.ID, sum[:]); err != nil {
					return fmt.Errorf("record intent %s (outcome uncertain): %w", s.ID, err)
				}
			}
			if actual == s.Before {
				if _, err := conn.ExecContext(ctx, s.SQL); err != nil {
					return fmt.Errorf("DDL step %s remains dirty: %w", s.ID, err)
				}
			}
			actual, err = Definition(ctx, conn, r.plan.Database, s.Kind, s.Name)
			if err != nil {
				return err
			}
			if actual != s.After {
				return fmt.Errorf("verify step %s: %w", s.ID, ErrDrift)
			}
			result, err := conn.ExecContext(ctx, "UPDATE "+schema+".`"+r.ledger+"` SET state='applied',applied_at=UTC_TIMESTAMP(6) WHERE step_no=? AND checksum=? AND state='dirty'", i+1, sum[:])
			if err != nil {
				return fmt.Errorf("record applied %s (outcome uncertain): %w", s.ID, err)
			}
			n, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return ErrLineage
			}
		}
		return nil
	})
}
