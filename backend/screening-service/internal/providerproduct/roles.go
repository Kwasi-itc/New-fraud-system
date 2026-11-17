package providerproduct

import (
	"context"
	_ "embed"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed roles.sql
var roleGrants string

// ProvisionLogin creates a bounded service login, never alters an existing
// password, and verifies the supplied credential against the actual server.
// Repeated setup fails closed if someone changed its privileges or password.
func ProvisionLogin(ctx context.Context, db *pgxpool.Pool, name, password, capability string) error {
	if !regexp.MustCompile(`^provider_[a-z][a-z0-9_]{0,40}$`).MatchString(name) || len(password) < 32 || len(password) > 200 || !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(password) {
		return errors.New("invalid restricted provider login or credential")
	}
	if capability != "screening_provider_reader" && capability != "screening_provider_collector" {
		return errors.New("unsupported provider capability")
	}
	tx, e := db.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(1800415344,24)`); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, roleGrants); e != nil {
		return e
	}
	var exists bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)`, name).Scan(&exists); e != nil {
		return e
	}
	if !exists {
		if _, e = tx.Exec(ctx, `CREATE ROLE `+pgx.Identifier{name}.Sanitize()+` LOGIN INHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD '`+password+`'`); e != nil {
			return errors.New("restricted provider login creation failed")
		}
		if _, e = tx.Exec(ctx, `GRANT `+pgx.Identifier{capability}.Sanitize()+` TO `+pgx.Identifier{name}.Sanitize()); e != nil {
			return e
		}
	}
	var safe bool
	if e = tx.QueryRow(ctx, `SELECT rolcanlogin AND NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole AND NOT rolreplication AND NOT rolbypassrls AND pg_has_role(oid,$2,'MEMBER') AND NOT EXISTS(SELECT 1 FROM pg_auth_members m JOIN pg_roles parent ON parent.oid=m.roleid WHERE m.member=pg_roles.oid AND parent.rolname<>$2) FROM pg_roles WHERE rolname=$1`, name, capability).Scan(&safe); e != nil || !safe {
		return errors.New("existing provider login has unexpected privileges")
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	cfg := db.Config()
	cfg.ConnConfig.User = name
	cfg.ConnConfig.Password = password
	cfg.MaxConns = 1
	cfg.MinConns = 0
	login, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		return errors.New("restricted provider login verification failed")
	}
	defer login.Close()
	if e = login.Ping(ctx); e != nil {
		return errors.New("existing provider login credential did not match; no password was changed")
	}
	return nil
}
