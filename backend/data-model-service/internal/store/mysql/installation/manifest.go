// Package installation builds reviewable data-model installation artifacts.
// It never connects to an application database or installs objects at startup.
package installation

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/metadata"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

// Definer is a dedicated, locked account, not an application login or installer.
// Provision and verify its locked status separately before applying the plan.
type Definer struct{ User, Host string }

// SQLAccount validates and quotes the dedicated principal for administrative
// statements. It never includes a password or permits the root account.
func (d Definer) SQLAccount() (string, error) { return d.account() }

var accountHost = regexp.MustCompile(`^[A-Za-z0-9_.:%-]{1,255}$`)

func (d Definer) account() (string, error) {
	user, err := mysqlstore.Identifier(d.User)
	if err != nil || len(d.User) > 32 || strings.EqualFold(d.User, "root") || !accountHost.MatchString(d.Host) {
		return "", errors.New("dedicated definer user and valid host required")
	}
	return user + "@`" + d.Host + "`", nil
}

// Statements returns the complete ordered structural lineage, without learned
// target definitions. IDs are versioned and order/checksum bound by the runner.
// A future upgrade must append reviewed steps; it must not regenerate history
// from changed source and pretend that the old lineage is unchanged.
func Statements(database string, definer Definer) ([]migrations.Step, error) {
	account, err := definer.account()
	if err != nil {
		return nil, err
	}
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	sql, err := metadata.Definitions(database)
	if err != nil {
		return nil, err
	}
	steps := make([]migrations.Step, 0, len(sql))
	for i, statement := range sql {
		words := strings.Fields(statement)
		if len(words) < 3 || (words[0] != "CREATE" && words[0] != "ALTER") {
			return nil, errors.New("unsupported metadata definition")
		}
		kind := words[1]
		if kind != "TABLE" && kind != "FUNCTION" && kind != "PROCEDURE" && kind != "TRIGGER" {
			return nil, errors.New("unsupported metadata object")
		}
		qualified := strings.SplitN(words[2], "(", 2)[0]
		if !strings.HasPrefix(qualified, schema+".") {
			return nil, errors.New("metadata object outside selected database")
		}
		name := strings.TrimPrefix(qualified, schema+".")
		if _, err := mysqlstore.Identifier(name); err != nil {
			return nil, err
		}
		if kind != "TABLE" {
			if words[0] != "CREATE" {
				return nil, errors.New("unsupported executable upgrade")
			}
			statement = strings.Replace(statement, "CREATE ", "CREATE DEFINER="+account+" ", 1)
		}
		steps = append(steps, migrations.Step{ID: fmt.Sprintf("data_model_v001_%04d", i+1), Kind: kind, Name: name, SQL: statement})
	}
	return steps, nil
}

// Plan binds shipped reference observations to the exact ordered source. Obtain
// these observations on a fresh isolated reference server at the pinned version,
// never by inspecting and accepting whatever exists on the production target.
// Include every intermediate definition, including the active-intent ALTER.
func Plan(database string, definer Definer, reference []migrations.Step) (migrations.Plan, error) {
	steps, err := Statements(database, definer)
	if err != nil {
		return migrations.Plan{}, err
	}
	if len(steps) != len(reference) {
		return migrations.Plan{}, errors.New("reference lineage length mismatch")
	}
	seen := map[string]bool{}
	for i, s := range steps {
		r := reference[i]
		if r.ID != s.ID || r.Kind != s.Kind || r.Name != s.Name || r.SQL != s.SQL {
			return migrations.Plan{}, fmt.Errorf("reference step %d differs from installation source", i+1)
		}
		key := s.Kind + ":" + s.Name
		if !seen[key] && r.Before != "" {
			return migrations.Plan{}, errors.New("initial installation requires absent objects")
		}
		seen[key] = true
		steps[i].Before, steps[i].After = r.Before, r.After
	}
	plan := migrations.Plan{Database: database, Steps: steps}
	if err := plan.Validate(); err != nil {
		return migrations.Plan{}, err
	}
	return plan, nil
}

// DefinerGrants is an explicit table/routine allowlist. No global, database-wide,
// DDL, DELETE or GRANT OPTION privileges are given to the locked definer. The
// UPDATE privilege is needed by BEFORE triggers assigning generated key IDs.
// Runtime users and physical DDL executors need separate reviewed policies.
func DefinerGrants(database string, definer Definer) ([]string, error) {
	account, err := definer.account()
	if err != nil {
		return nil, err
	}
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	steps, err := Statements(database, definer)
	if err != nil {
		return nil, err
	}
	grants := []string{}
	seen := map[string]bool{}
	for _, s := range steps {
		name, _ := mysqlstore.Identifier(s.Name)
		switch s.Kind {
		case "TABLE":
			if seen[s.Name] {
				continue
			}
			seen[s.Name] = true
			privileges := "SELECT, UPDATE, TRIGGER"
			if s.Name == "data_model_exact_keys" {
				privileges = "SELECT, INSERT"
			}
			if s.Name == "data_model_exact_key_buckets" {
				privileges = "SELECT, INSERT, UPDATE"
			}
			grants = append(grants, "GRANT "+privileges+" ON "+schema+"."+name+" TO "+account)
		case "FUNCTION", "PROCEDURE":
			grants = append(grants, "GRANT EXECUTE ON "+s.Kind+" "+schema+"."+name+" TO "+account)
		}
	}
	return grants, nil
}
