package mysqlauth

import (
	"context"
	"errors"
	"strings"
)

// Runtime readiness verifies the effective account's complete table grants.
// Installer/operator pools deliberately do not select a runtime role.
func (s *Store) verifyRole(ctx context.Context) error {
	if s.role == "" {
		return nil
	}
	if s.role != "api" && s.role != "worker" {
		return errors.New("invalid auth role")
	}
	var grantee string
	if e := s.DB.QueryRowContext(ctx, `SELECT CONCAT(CHAR(39),SUBSTRING_INDEX(CURRENT_USER(),'@',1),CHAR(39),'@',CHAR(39),SUBSTRING_INDEX(CURRENT_USER(),'@',-1),CHAR(39))`).Scan(&grantee); e != nil {
		return e
	}
	var broad int
	if e := s.DB.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM information_schema.USER_PRIVILEGES WHERE GRANTEE=? AND PRIVILEGE_TYPE<>'USAGE')+(SELECT COUNT(*) FROM information_schema.SCHEMA_PRIVILEGES WHERE GRANTEE=?)`, grantee, grantee).Scan(&broad); e != nil {
		return e
	}
	if broad != 0 {
		return errors.New("auth runtime must use restricted table grants")
	}
	grants, e := RuntimeGrants(s.database, "local_auth_"+s.role, s.role == "worker")
	if e != nil {
		return e
	}
	expected := map[string]map[string]bool{}
	for _, g := range grants {
		parts := strings.Fields(g)
		table := strings.SplitN(parts[3], ".", 2)[1]
		if expected[table] == nil {
			expected[table] = map[string]bool{}
		}
		for _, privilege := range strings.Split(parts[1], ",") {
			expected[table][privilege] = true
		}
	}
	rows, e := s.DB.QueryContext(ctx, "SELECT TABLE_SCHEMA,TABLE_NAME,PRIVILEGE_TYPE FROM information_schema.TABLE_PRIVILEGES WHERE GRANTEE=? LIMIT 513", grantee)
	if e != nil {
		return e
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var database, table, privilege string
		if e = rows.Scan(&database, &table, &privilege); e != nil {
			return e
		}
		count++
		if count > 512 || database != s.database || !expected[table][privilege] {
			return errors.New("unexpected auth runtime privilege")
		}
		delete(expected[table], privilege)
	}
	if e = rows.Err(); e != nil {
		return e
	}
	for _, privileges := range expected {
		if len(privileges) != 0 {
			return errors.New("required auth runtime grant missing")
		}
	}
	return nil
}
