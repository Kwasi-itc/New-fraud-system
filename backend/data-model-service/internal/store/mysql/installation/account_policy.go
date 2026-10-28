package installation

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"sort"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

// AccountPolicy is a complete direct-privilege allowlist for one dedicated
// principal. No global/database wildcard grants, roles or grant options are
// supported. Provisioning credentials remains the deployment secret workflow.
type AccountPolicy struct {
	User    string        `json:"user"`
	Host    string        `json:"host"`
	Locked  bool          `json:"locked"`
	Objects []ObjectGrant `json:"objects"`
}

type ObjectGrant struct {
	Kind       string   `json:"kind"`
	Name       string   `json:"name"`
	Privileges []string `json:"privileges"`
	// Columns grants a privilege only on these named columns. A privilege must
	// not also appear in Privileges; table-wide access would hide that boundary.
	Columns map[string][]string `json:"columns"`
}

// GrantStatements renders only typed, individual-object grants in MySQL's
// canonical privilege order. The policy is reviewed deployment input, not HTTP.
func GrantStatements(database string, policy AccountPolicy) ([]string, error) {
	account, err := (Definer{policy.User, policy.Host}).account()
	if err != nil {
		return nil, err
	}
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if len(policy.Objects) == 0 || len(policy.Objects) > 10000 {
		return nil, errors.New("1..10000 explicit object grants required")
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(policy.Objects))
	bytes := len("GRANT USAGE ON *.* TO " + account)
	for _, object := range policy.Objects {
		name, err := mysqlstore.Identifier(object.Name)
		if err != nil {
			return nil, err
		}
		key := object.Kind + ":" + object.Name
		if seen[key] {
			return nil, errors.New("duplicate grant object")
		}
		seen[key] = true
		order := []string{"SELECT", "INSERT", "UPDATE", "DELETE", "CREATE", "DROP", "REFERENCES", "INDEX", "ALTER", "CREATE VIEW", "SHOW VIEW", "TRIGGER"}
		prefix := ""
		switch object.Kind {
		case "TABLE":
		case "FUNCTION", "PROCEDURE":
			order = []string{"EXECUTE"}
			prefix = object.Kind + " "
		default:
			return nil, errors.New("invalid grant object kind")
		}
		requested := map[string]bool{}
		if len(object.Privileges)+len(object.Columns) == 0 || len(object.Privileges)+len(object.Columns) > len(order) {
			return nil, errors.New("nonempty bounded privileges required")
		}
		for _, privilege := range object.Privileges {
			if requested[privilege] {
				return nil, errors.New("duplicate privilege")
			}
			requested[privilege] = true
		}
		columnPrivileges := map[string]string{}
		columnCount := 0
		for privilege, columns := range object.Columns {
			if object.Kind != "TABLE" || (privilege != "SELECT" && privilege != "INSERT" && privilege != "UPDATE" && privilege != "REFERENCES") || requested[privilege] {
				return nil, errors.New("invalid or overlapping column privilege")
			}
			columnCount += len(columns)
			if len(columns) == 0 || columnCount > 4096 {
				return nil, errors.New("nonempty bounded column grants required")
			}
			quoted := make([]string, 0, len(columns))
			seenColumns := map[string]bool{}
			for _, column := range columns {
				name, err := mysqlstore.Identifier(column)
				if err != nil {
					return nil, err
				}
				if seenColumns[column] {
					return nil, errors.New("duplicate grant column")
				}
				seenColumns[column] = true
				quoted = append(quoted, name)
			}
			sort.Strings(quoted)
			columnPrivileges[privilege] = privilege + " (" + strings.Join(quoted, ", ") + ")"
		}
		var sorted []string
		for _, privilege := range order {
			if requested[privilege] {
				sorted = append(sorted, privilege)
				delete(requested, privilege)
			}
			if columns, ok := columnPrivileges[privilege]; ok {
				sorted = append(sorted, columns)
			}
		}
		if len(requested) != 0 {
			return nil, errors.New("unsupported privilege")
		}
		statement := "GRANT " + strings.Join(sorted, ", ") + " ON " + prefix + schema + "." + name + " TO " + account
		bytes += len(statement)
		if bytes > 1<<20 {
			return nil, errors.New("account policy exceeds byte limit")
		}
		result = append(result, statement)
	}
	return result, nil
}

// VerifyAccountPolicy requires privileged catalog visibility. It checks the
// complete account, including unrelated extra privileges and inherited roles,
// not merely whether a desired grant happens to be present. Operationally
// exclude concurrent privilege administration while verifying and publishing.
func VerifyAccountPolicy(ctx context.Context, conn *sql.Conn, database string, policy AccountPolicy) error {
	return inspectAccountPolicy(ctx, conn, database, policy, false)
}

// allowMissing accepts only whole missing object grants, as can result from an
// interrupted sequence of atomic GRANT statements. It never accepts broadened
// privileges or a different partial grant on an existing object.
func inspectAccountPolicy(ctx context.Context, conn *sql.Conn, database string, policy AccountPolicy, allowMissing bool) error {
	if conn == nil {
		return errors.New("privileged inspection connection required")
	}
	if err := verifyNoMandatoryRoles(ctx, conn); err != nil {
		return err
	}
	grants, err := GrantStatements(database, policy)
	if err != nil {
		return err
	}
	return inspectRenderedAccountPolicy(ctx, conn, policy, grants, allowMissing)
}

// Only typed policies in this package may supply additional rendered grants.
// Ordinary AccountPolicy never accepts global capabilities.
func inspectRenderedAccountPolicy(ctx context.Context, conn *sql.Conn, policy AccountPolicy, grants []string, allowMissing bool) error {
	account, _ := (Definer{policy.User, policy.Host}).account()
	var locked string
	if err := conn.QueryRowContext(ctx, "SELECT account_locked FROM mysql.user WHERE User=? AND Host=?", policy.User, policy.Host).Scan(&locked); err != nil {
		return err
	}
	want := "N"
	if policy.Locked {
		want = "Y"
	}
	if locked != want {
		return errors.New("account lock status differs from reviewed policy")
	}
	expected := map[string]bool{"GRANT USAGE ON *.* TO " + account: true}
	for _, grant := range grants {
		expected[grant] = true
	}
	rows, err := conn.QueryContext(ctx, "SHOW GRANTS FOR "+account)
	if err != nil {
		return err
	}
	defer rows.Close()
	count, bytes := 0, 0
	for rows.Next() {
		var grant string
		if err := rows.Scan(&grant); err != nil {
			return err
		}
		count++
		bytes += len(grant)
		if count > len(grants)+1 || bytes > 1<<20 {
			return errors.New("account grants exceed inspection bounds")
		}
		grant = canonicalGrantColumns(grant)
		if !expected[grant] {
			return errors.New("account grants differ from reviewed policy")
		}
		delete(expected, grant)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if expected["GRANT USAGE ON *.* TO "+account] || (!allowMissing && len(expected) != 0) {
		return errors.New("required account privileges missing")
	}
	return nil
}

// SHOW GRANTS column order is not a privilege boundary. Normalize only a strict
// quoted-column list for supported privileges; leave every other byte intact so
// extra privileges, scope, roles, grant options or unknown syntax fail closed.
var grantColumnList = regexp.MustCompile("(SELECT|INSERT|UPDATE|REFERENCES) \\((`[a-z][a-z0-9_]{0,63}`)(, `[a-z][a-z0-9_]{0,63}`)*\\)")

func canonicalGrantColumns(grant string) string {
	return grantColumnList.ReplaceAllStringFunc(grant, func(part string) string {
		start := strings.Index(part, " (") + 2
		columns := strings.Split(part[start:len(part)-1], ", ")
		sort.Strings(columns)
		return part[:start] + strings.Join(columns, ", ") + ")"
	})
}

// Mandatory server roles can confer privileges beyond direct account grants.
// This deployment contract excludes them rather than assuming SHOW GRANTS FOR
// another principal describes every inherited server-wide capability.
func verifyNoMandatoryRoles(ctx context.Context, conn *sql.Conn) error {
	var roles string
	if err := conn.QueryRowContext(ctx, "SELECT @@GLOBAL.mandatory_roles").Scan(&roles); err != nil {
		return err
	}
	if strings.TrimSpace(roles) != "" {
		return errors.New("mandatory roles are outside the reviewed direct-grant policy")
	}
	return nil
}
