package installation

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

// RecordInstallerPolicy is the explicit, temporary capability to install one
// initial record table and its dedicated-definer triggers, or to alter that
// guarded table using NativeChanges. It is not a runtime role. Initial creation
// needs SET_ANY_DEFINER, which cannot be restricted to one named definer, so
// deployment must keep these credentials isolated and deactivate the account
// before publication. No automatic account provisioning or global setting change
// is performed by these helpers.
type RecordInstallerPolicy struct {
	Principal Definer
	Table     string
	// NativeChanges enables table ALTER/INDEX with inspection of existing
	// triggers, but grants no global authority to install another definer.
	NativeChanges bool
}

func (p RecordInstallerPolicy) Account(locked bool) (AccountPolicy, error) {
	if _, err := p.Principal.SQLAccount(); err != nil {
		return AccountPolicy{}, err
	}
	if _, err := mysqlstore.Identifier(p.Table); err != nil || !strings.HasPrefix(p.Table, "tenant_") {
		return AccountPolicy{}, errors.New("registered physical record table required")
	}
	privileges := []string{"SELECT", "CREATE", "TRIGGER"}
	if p.NativeChanges {
		privileges = append(privileges, "ALTER", "INDEX")
	}
	return AccountPolicy{User: p.Principal.User, Host: p.Principal.Host, Locked: locked, Objects: []ObjectGrant{
		{Kind: "TABLE", Name: p.Table, Privileges: privileges},
		{Kind: "TABLE", Name: "data_model_exact_keys", Privileges: []string{"REFERENCES"}},
		{Kind: "TABLE", Name: "data_model_physical_tables", Privileges: []string{"SELECT"}, Columns: map[string][]string{"UPDATE": {"table_id"}}},
		{Kind: "TABLE", Name: "data_model_schema_change_intents", Privileges: []string{"SELECT"}, Columns: map[string][]string{"UPDATE": {"state", "last_error_code", "updated_at"}}},
		{Kind: "TABLE", Name: "data_model_schema_change_steps", Privileges: []string{"SELECT", "INSERT"}, Columns: map[string][]string{"UPDATE": {"state", "applied_at"}}},
	}}, nil
}

// ActiveGrants renders a reviewable installation policy; it does not execute it.
// SUPER, SYSTEM_USER, ALLOW_NONEXISTENT_DEFINER, GRANT OPTION, broad DML, routine
// EXECUTE and wildcard object privileges are deliberately absent.
func (p RecordInstallerPolicy) ActiveGrants(database string) ([]string, error) {
	a, err := p.Account(false)
	if err != nil {
		return nil, err
	}
	grants, err := GrantStatements(database, a)
	if err != nil {
		return nil, err
	}
	account, _ := p.Principal.SQLAccount()
	if p.NativeChanges {
		return grants, nil
	}
	return append(grants, "GRANT SET_ANY_DEFINER ON *.* TO "+account), nil
}

func (p RecordInstallerPolicy) VerifyActive(ctx context.Context, conn *sql.Conn, database string) error {
	if conn == nil {
		return errors.New("privileged inspection connection required")
	}
	a, err := p.Account(false)
	if err != nil {
		return err
	}
	grants, err := p.ActiveGrants(database)
	if err != nil {
		return err
	}
	if err := verifyNoMandatoryRoles(ctx, conn); err != nil {
		return err
	}
	if err := inspectRenderedAccountPolicy(ctx, conn, a, grants, false); err != nil {
		return err
	}
	return VerifyRecordInstallationSettings(ctx, conn)
}

// VerifyRecordInstallationSettings rejects configurations requiring SUPER and
// statement/mixed replication of UUID allocation. It never changes the server.
func VerifyRecordInstallationSettings(ctx context.Context, conn *sql.Conn) error {
	if conn == nil {
		return errors.New("installation connection required")
	}
	var logging, trust bool
	var format string
	if err := conn.QueryRowContext(ctx, "SELECT @@GLOBAL.log_bin,@@GLOBAL.log_bin_trust_function_creators,@@GLOBAL.binlog_format").Scan(&logging, &trust, &format); err != nil {
		return err
	}
	if logging && (!trust || format != "ROW") {
		return errors.New("record trigger installation requires reviewed ROW binary logging and trusted function creators, or disabled binary logging")
	}
	return nil
}

// VerifyInactive is a publication precondition: credentials remain locked, no
// old session survives, and no global capability remains (initial creation must
// revoke SET_ANY_DEFINER; native changes never grant it). The exact object
// grants remain for auditable recovery; unlocking/regranting is an explicit
// deployment action and never a publisher repair.
func (p RecordInstallerPolicy) VerifyInactive(ctx context.Context, conn *sql.Conn, database string) error {
	a, err := p.Account(true)
	if err != nil {
		return err
	}
	if err := VerifyAccountPolicy(ctx, conn, database, a); err != nil {
		return err
	}
	return VerifyNoAccountSessions(ctx, conn, p.Principal)
}

// VerifyNoAccountSessions conservatively checks all hosts for a dedicated user.
// Call after verifying ACCOUNT LOCK and exact grants, with privilege changes
// operationally excluded. A locked account may still have old live sessions.
func VerifyNoAccountSessions(ctx context.Context, conn *sql.Conn, principal Definer) error {
	if conn == nil {
		return errors.New("privileged inspection connection required")
	}
	if _, err := principal.SQLAccount(); err != nil {
		return err
	}
	var enabled, sessions bool
	if err := conn.QueryRowContext(ctx, "SELECT @@GLOBAL.performance_schema").Scan(&enabled); err != nil {
		return err
	}
	if !enabled {
		return errors.New("performance_schema required to inspect account sessions")
	}
	if err := conn.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM performance_schema.threads WHERE TYPE='FOREGROUND' AND PROCESSLIST_USER=?)", principal.User).Scan(&sessions); err != nil {
		return err
	}
	if sessions {
		return errors.New("dedicated account has existing sessions")
	}
	return nil
}
