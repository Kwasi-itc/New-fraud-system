package installation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// CreateDefiner returns a separate account-provisioning statement. It fails if
// the account exists rather than adopting an account with unknown privileges.
// No password is generated or stored: this identity must never authenticate.
func CreateDefiner(definer Definer) (string, error) {
	account, err := definer.account()
	if err != nil {
		return "", err
	}
	return "CREATE USER " + account + " ACCOUNT LOCK", nil
}

// VerifyDefiner requires privileged installation visibility and an exact direct
// grant allowlist. Extra grants, roles, grant options, missing grants and unlocked
// accounts fail closed. This checks one account, not every application role or
// physical-table grant. Run under operational exclusion of privilege changes.
func VerifyDefiner(ctx context.Context, conn *sql.Conn, database string, definer Definer) error {
	if conn == nil {
		return errors.New("installer connection required")
	}
	if err := verifyNoMandatoryRoles(ctx, conn); err != nil {
		return err
	}
	account, err := definer.account()
	if err != nil {
		return err
	}
	grants, err := DefinerGrants(database, definer)
	if err != nil {
		return err
	}
	var locked string
	if err := conn.QueryRowContext(ctx, "SELECT account_locked FROM mysql.user WHERE User=? AND Host=?", definer.User, definer.Host).Scan(&locked); err != nil {
		return fmt.Errorf("inspect definer lock: %w", err)
	}
	if locked != "Y" {
		return errors.New("definer account must remain locked")
	}
	expected := map[string]bool{"GRANT USAGE ON *.* TO " + account: true}
	for _, g := range grants {
		expected[g] = true
	}
	rows, err := conn.QueryContext(ctx, "SHOW GRANTS FOR "+account)
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		if count > len(grants)+1 {
			return errors.New("unexpected definer grants")
		}
		var grant string
		if err := rows.Scan(&grant); err != nil {
			return err
		}
		if !expected[grant] {
			return errors.New("definer privileges differ from reviewed allowlist")
		}
		delete(expected, grant)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(expected) != 0 {
		return errors.New("required definer privileges missing")
	}
	return nil
}
