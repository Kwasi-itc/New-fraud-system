// export-mysql-fixture emits owner-authored source for disposable integration
// fixtures. It never connects to a database and is not a deployment artifact.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

type manifest struct {
	Steps  []migrations.Step
	Create []string
	Grants []string
	Drop   []string
}

func build(database, suffix string) (manifest, error) {
	var m manifest
	if !regexp.MustCompile(`^ingestion_full_test_[a-f0-9]{32}$`).MatchString(database) || !regexp.MustCompile(`^[a-f0-9]{16}$`).MatchString(suffix) {
		return m, errors.New("generated isolated fixture identity required")
	}
	policies := []func(installation.Definer) installation.AccountPolicy{nil, installation.CreationReceiptDefinerPolicy, installation.OwnerResultDefinerPolicy, installation.RecordLookupDefinerPolicy, installation.ContractLookupDefinerPolicy}
	builders := []func(string, installation.Definer) ([]migrations.Step, error){installation.Statements, installation.CreationReceiptUpgradeStatements, installation.OwnerResultUpgradeStatements, installation.RecordLookupUpgradeStatements, installation.ContractLookupUpgradeStatements}
	for i, builder := range builders {
		d := installation.Definer{User: fmt.Sprintf("if%d_%s", i, suffix), Host: "%"}
		create, err := installation.CreateDefiner(d)
		if err != nil {
			return m, err
		}
		account, err := d.SQLAccount()
		if err != nil {
			return m, err
		}
		m.Create = append(m.Create, create)
		m.Drop = append(m.Drop, "DROP USER "+account)
		steps, err := builder(database, d)
		if err != nil {
			return m, err
		}
		m.Steps = append(m.Steps, steps...)
		var grants []string
		if i == 0 {
			grants, err = installation.DefinerGrants(database, d)
			upgrade, e := installation.DeliveryRetryUpgradeStatement(database)
			if e != nil {
				return m, e
			}
			m.Steps = append(m.Steps, upgrade)
		} else {
			grants, err = installation.GrantStatements(database, policies[i](d))
		}
		if err != nil {
			return m, err
		}
		m.Grants = append(m.Grants, grants...)
	}
	return m, nil
}
func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "generated database and account suffix required")
		os.Exit(1)
	}
	m, err := build(os.Args[1], os.Args[2])
	if err == nil {
		err = json.NewEncoder(os.Stdout).Encode(m)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
